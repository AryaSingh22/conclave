package unordered

// PROVENANCE PIN: SigVerificationDecorator below is forked from
// github.com/cosmos/cosmos-sdk@v0.50.15, file x/auth/ante/sigverify.go
// (the SigVerificationDecorator type and its AnteHandle only; shared
// helpers GetSignerAcc / OnlyLegacyAminoSigners are imported, not copied).
//
// DELTA (one sentence): skips the sequence-equality check iff the tx
// carries the unordered extension — verifying AMINO/TEXTUAL signatures
// against the tx's own claimed sequence instead of account state —
// otherwise byte-identical. Replay safety for unordered txs comes from
// the UnorderedTxDecorator's dedup cache (SPEC §6.7), never from
// sequences. The behavioral pin is the conformance suite
// (app/unordered_conformance_test.go); this header is the provenance pin.
//
// CONCLAVE-TODO(phase-3): replace this fork with the SDK's native ADR-070
// unordered-tx support on the SDK-family bump. Owned by task 3.3b (SDK-family
// migration), STOP #1. RE-POINTED from "task 3.3" at task 3.3a (D-099): D-096
// split 3.3, and the old referent survived the split in-tree. 3.3a is the
// toolchain/security half and explicitly does NOT touch the family, so it
// cannot retire this fork — naming it here would have been worse than the
// stale reference it replaced.
//
// Checkpoint history — the fork survives on evidence, not inertia:
//   - task 2.2 (D-077, updating D-060) RE-RESOLVED stay-on-v0.50: block-sdk
//     frozen with no v0.53 line (a bump forfeits the §6.7 laned mempool) and
//     the v0.53 wasmd line's patches need go 1.24 (the D-066 kyber collision).
//   - task 2.5 (D-081) RAN the next checkpoint and CLOSED it: block-sdk is
//     still v2.1.5 with no v2.1.6 and no /v3, so flip-condition (a) still
//     fails. New input for 3.3 — on the v0.53 path the toolchain collision now
//     comes from wasmd (v0.61.14+ → go 1.24), not from ibc-go (v10.7.0 floors
//     at go 1.23.8, inside the ceiling).
//
// So the fork's retirement is 3.3's to decide alongside the block-sdk
// disposition (fork-and-maintain vs app-side lanes), because both answers move
// together. We migrate deliberately, not opportunistically.

import (
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"

	errorsmod "cosmossdk.io/errors"
	txsigning "cosmossdk.io/x/tx/signing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	"github.com/AryaSingh22/conclave/app/unordered/types"
)

// SigVerificationDecorator verifies all signatures for a tx and returns an
// error if any are invalid. Note, the SigVerificationDecorator will not
// check signatures on ReCheck.
//
// CONTRACT: Pubkeys are set in context for all signers before this decorator runs
// CONTRACT: Tx must implement SigVerifiableTx interface
type SigVerificationDecorator struct {
	ak              ante.AccountKeeper
	signModeHandler *txsigning.HandlerMap
}

// NewSigVerificationDecorator creates the unordered-aware fork of the
// standard signature verification decorator (see provenance pin above).
func NewSigVerificationDecorator(ak ante.AccountKeeper, signModeHandler *txsigning.HandlerMap) SigVerificationDecorator {
	return SigVerificationDecorator{
		ak:              ak,
		signModeHandler: signModeHandler,
	}
}

var _ sdk.AnteDecorator = SigVerificationDecorator{}

// AnteHandle is the forked v0.50.15 implementation; the FORK DELTA
// comments mark the only divergences.
func (svd SigVerificationDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	sigTx, ok := tx.(authsigning.Tx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
	}

	// FORK DELTA: detect the unordered extension once per tx.
	_, unordered, err := types.TimeoutFromTx(tx)
	if err != nil {
		return ctx, err
	}

	// stdSigs contains the sequence number, account number, and signatures.
	// When simulating, this would just be a 0-length slice.
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return ctx, err
	}

	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}

	// check that signer length and signature length are the same
	if len(sigs) != len(signers) {
		return ctx, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "invalid number of signer;  expected: %d, got %d", len(signers), len(sigs))
	}

	for i, sig := range sigs {
		acc, err := ante.GetSignerAcc(ctx, svd.ak, signers[i])
		if err != nil {
			return ctx, err
		}

		// retrieve pubkey
		pubKey := acc.GetPubKey()
		if !simulate && pubKey == nil {
			return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "pubkey on account is not set")
		}

		// Check account sequence number.
		// FORK DELTA: skipped iff the tx is unordered — replay protection
		// is the SPEC §6.7 dedup cache; ordered txs keep the upstream
		// check byte-identical.
		if !unordered && sig.Sequence != acc.GetSequence() {
			return ctx, errorsmod.Wrapf(
				sdkerrors.ErrWrongSequence,
				"account sequence mismatch, expected %d, got %d", acc.GetSequence(), sig.Sequence,
			)
		}

		// retrieve signer data
		genesis := ctx.BlockHeight() == 0
		chainID := ctx.ChainID()
		var accNum uint64
		if !genesis {
			accNum = acc.GetAccountNumber()
		}

		// no need to verify signatures on recheck tx
		if !simulate && !ctx.IsReCheckTx() && ctx.IsSigverifyTx() {
			anyPk, _ := codectypes.NewAnyWithValue(pubKey)

			// FORK DELTA: unordered txs verify against the sequence the
			// wallet actually signed (sig.Sequence); only SIGN_MODE
			// AMINO/TEXTUAL embed it in the sign doc — DIRECT covers the
			// raw auth_info bytes either way.
			signSeq := acc.GetSequence()
			if unordered {
				signSeq = sig.Sequence
			}

			signerData := txsigning.SignerData{
				Address:       acc.GetAddress().String(),
				ChainID:       chainID,
				AccountNumber: accNum,
				Sequence:      signSeq,
				PubKey: &anypb.Any{
					TypeUrl: anyPk.TypeUrl,
					Value:   anyPk.Value,
				},
			}
			adaptableTx, ok := tx.(authsigning.V2AdaptableTx)
			if !ok {
				return ctx, fmt.Errorf("expected tx to implement V2AdaptableTx, got %T", tx)
			}
			txData := adaptableTx.GetSigningTxData()
			err = authsigning.VerifySignature(ctx, pubKey, signerData, sig.Data, svd.signModeHandler, txData)
			if err != nil {
				var errMsg string
				if ante.OnlyLegacyAminoSigners(sig.Data) {
					// If all signers are using SIGN_MODE_LEGACY_AMINO, we rely on VerifySignature to check account sequence number,
					// and therefore communicate sequence number as a potential cause of error.
					errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d), sequence (%d) and chain-id (%s)", accNum, acc.GetSequence(), chainID)
				} else {
					errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d) and chain-id (%s): (%s)", accNum, chainID, err.Error())
				}
				return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, errMsg)
			}
		}
	}

	return next(ctx, tx, simulate)
}

// IncrementSequenceDecorator wraps the standard sequence-increment
// decorator and skips it for unordered txs: their account sequence is
// untouched, so any number of them can be in flight from one key while
// ordered txs from the same key keep exact upstream semantics (SPEC §6.7).
type IncrementSequenceDecorator struct {
	inner sdk.AnteDecorator
}

// NewIncrementSequenceDecorator wraps the standard
// ante.IncrementSequenceDecorator with the unordered skip.
func NewIncrementSequenceDecorator(inner sdk.AnteDecorator) IncrementSequenceDecorator {
	return IncrementSequenceDecorator{inner: inner}
}

var _ sdk.AnteDecorator = IncrementSequenceDecorator{}

// AnteHandle skips sequence increments for unordered txs.
func (d IncrementSequenceDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if _, unordered, err := types.TimeoutFromTx(tx); err != nil {
		return ctx, err
	} else if unordered {
		return next(ctx, tx, simulate)
	}
	return d.inner.AnteHandle(ctx, tx, simulate, next)
}
