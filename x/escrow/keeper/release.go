package keeper

import (
	"crypto/sha256"
	"crypto/subtle"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/escrow/types"
)

// hashEq reports whether SHA-256(preimage) equals the committed 32-byte hash,
// in constant time (D-073: HASHLOCK and OPTIMISTIC both gate release on
// SHA-256(preimage)==committed; SHA-256 is fresh-randomness-free, §11.6, and
// hashing arbitrary bytes never panics, §11.5).
func hashEq(preimage, committed []byte) bool {
	sum := sha256.Sum256(preimage)
	return subtle.ConstantTimeCompare(sum[:], committed) == 1
}

// payTo resolves a sigil to its controller address and pays it from the escrow
// module account. A zero/empty amount is a no-op (a NONE-stub or zero-bond
// escrow moves no funds).
func (k Keeper) payTo(ctx sdk.Context, chamberID uint32, sigil string, coins sdk.Coins) error {
	if coins.IsZero() {
		return nil
	}
	addr, err := k.sigilKeeper.ControllerAddress(ctx, chamberID, sigil)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, addr, coins)
}

// releaseFunds pays the escrowed amount to the payee (seller) and, for
// OPTIMISTIC, returns the seller's dispute bond.
func (k Keeper) releaseFunds(ctx sdk.Context, esc types.Escrow, class conditionClass) error {
	if err := k.payTo(ctx, esc.ChamberId, esc.PayeeSigil, esc.Amount); err != nil {
		return err
	}
	if class == classOptimistic {
		if err := k.payTo(ctx, esc.ChamberId, esc.PayeeSigil, types.SellerBondCoins(esc.Amount)); err != nil {
			return err
		}
	}
	return nil
}

// refundFunds returns the escrowed amount to the payer (buyer). For an
// OPTIMISTIC lost dispute it additionally slashes the seller bond to the buyer
// and fires the SPEC §6.1 lost-dispute reputation penalty on the seller
// (PenalizeLostDispute). HASHLOCK timeout refunds carry no bond.
func (k Keeper) refundFunds(ctx sdk.Context, esc types.Escrow, class conditionClass) error {
	if err := k.payTo(ctx, esc.ChamberId, esc.PayerSigil, esc.Amount); err != nil {
		return err
	}
	if class == classOptimistic {
		if err := k.payTo(ctx, esc.ChamberId, esc.PayerSigil, types.SellerBondCoins(esc.Amount)); err != nil {
			return err
		}
		if err := k.sigilKeeper.PenalizeLostDispute(ctx, esc.ChamberId, esc.PayeeSigil); err != nil {
			return err
		}
	}
	return nil
}

// deleteSweepIdx removes an escrow's current sweep-index entry (keyed by its
// current TimeoutHeight). Idempotent for a NONE stub that never had one.
func (k Keeper) deleteSweepIdx(ctx sdk.Context, esc types.Escrow) error {
	idBz, err := types.IDBytes(esc.Id)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Delete(types.SweepIdxKey(esc.TimeoutHeight, idBz))
}

// commitTerminal writes the terminal state, clears the sweep-index entry, and
// emits the terminal event. The record and participant index are retained so
// the escrow stays queryable (spirit of D-017).
func (k Keeper) commitTerminal(ctx sdk.Context, esc types.Escrow, to types.EscrowState, eventType string) error {
	if err := k.deleteSweepIdx(ctx, esc); err != nil {
		return err
	}
	esc.State = to
	if err := k.setEscrow(ctx, esc); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		eventType,
		sdk.NewAttribute(types.AttributeKeyEscrowID, esc.Id),
		sdk.NewAttribute(types.AttributeKeyState, esc.State.String()),
	))
	return nil
}

// finalizeRelease pays out a release and commits the terminal RELEASED state.
func (k Keeper) finalizeRelease(ctx sdk.Context, esc types.Escrow, class conditionClass, to types.EscrowState) error {
	if err := k.releaseFunds(ctx, esc, class); err != nil {
		return err
	}
	return k.commitTerminal(ctx, esc, to, types.EventTypeEscrowReleased)
}

// finalizeRefund pays out a refund and commits the terminal REFUNDED state.
func (k Keeper) finalizeRefund(ctx sdk.Context, esc types.Escrow, class conditionClass, to types.EscrowState) error {
	if err := k.refundFunds(ctx, esc, class); err != nil {
		return err
	}
	return k.commitTerminal(ctx, esc, to, types.EventTypeEscrowRefunded)
}

// ReleaseHashlock executes the HASHLOCK ACTIVE→RELEASED transition: the
// presented preimage must hash to the committed payload hash and the release
// must be pre-timeout. Knowledge of the preimage authorizes release (standard
// HTLC; signer unconstrained — D-073). On success the escrowed payment pays the
// payee (seller).
//
// implements SPEC §6.5 HASHLOCK release.
func (k Keeper) ReleaseHashlock(ctx sdk.Context, esc types.Escrow, preimage []byte) error {
	h := esc.Condition.GetHashlock()
	if h == nil {
		return types.ErrWrongClass
	}
	if !hashEq(preimage, h.PayloadHash) {
		return types.ErrPreimageMismatch
	}
	if uint64(ctx.BlockHeight()) >= esc.TimeoutHeight {
		return types.ErrTimedOut
	}
	to, err := nextState(classHashlock, esc.State, evtHashlockRelease)
	if err != nil {
		return err
	}
	return k.finalizeRelease(ctx, esc, classHashlock, to)
}

// RevealOptimistic executes the OPTIMISTIC DISPUTED→RELEASED transition: the
// seller defends a dispute by revealing the content preimage of the
// buyer-agreed committed hash. A hash match proves delivery (the challenge
// fails); the payment and the returned bond pay the payee (seller). Only valid
// while DISPUTED (the gate enforces this).
//
// implements SPEC §6.5 OPTIMISTIC dispute defense (D-073 seller-reveal model).
func (k Keeper) RevealOptimistic(ctx sdk.Context, esc types.Escrow, preimage []byte) error {
	idBz, err := types.IDBytes(esc.Id)
	if err != nil {
		return err
	}
	commit, err := k.getCommit(ctx, idBz)
	if err != nil {
		return err
	}
	if !hashEq(preimage, commit) {
		return types.ErrPreimageMismatch
	}
	to, err := nextState(classOptimistic, esc.State, evtOptimisticReveal)
	if err != nil {
		return err
	}
	return k.finalizeRelease(ctx, esc, classOptimistic, to)
}
