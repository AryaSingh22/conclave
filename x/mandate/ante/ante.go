// Package ante implements the Conclave agent-safety chokepoint: the
// MandateDecorator and FeeAbstractionDecorator (SPEC §2, §6.2). Mandate
// enforcement happens here, before any message executes (SPEC §1.1 goal 3).
package ante

import (
	errorsmod "cosmossdk.io/errors"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/AryaSingh22/conclave/x/mandate/keeper"
	"github.com/AryaSingh22/conclave/x/mandate/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// MandateDecorator enforces SPEC §6.2's exact six-step algorithm against
// every tx signer, before execution and identically in CheckTx and
// DeliverTx (the optimistic window charge in both modes is what bounds a
// stolen key across many parallel unordered txs in one block).
type MandateDecorator struct {
	keeper keeper.Keeper
}

// NewMandateDecorator creates the SPEC §6.2 ante decorator. It no longer
// takes a PriceSource: the spend intent of a MsgSubmitIntent is now the
// declared spend_commitment + bond (D-065), read from the plaintext
// envelope, so the ante never values give legs — uniformly in stub and
// encrypted mode. The give-leg valuation moved to the Reveal cross-check
// (x/veil), which keeps its own PriceSource.
func NewMandateDecorator(k keeper.Keeper) MandateDecorator {
	return MandateDecorator{keeper: k}
}

var _ sdk.AnteDecorator = MandateDecorator{}

// AnteHandle implements SPEC §6.2 "Ante enforcement — MandateDecorator
// (exact algorithm)".
func (d MandateDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		// non-signature txs never reach msg execution: the standard
		// decorators ahead of this one already require SigVerifiableTx
		return next(ctx, tx, simulate)
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}

	params, err := d.keeper.GetParams(ctx)
	if err != nil {
		return ctx, err
	}

	seen := make(map[string]bool, len(signers))
	for _, signerBz := range signers {
		signer := sdk.AccAddress(signerBz)
		if seen[signer.String()] {
			continue
		}
		seen[signer.String()] = true

		// step 1: no mandate index entry → root/controller key, pass through
		m, found, err := d.keeper.ActiveMandateByGrantee(ctx, keeper.Phase0Chamber(), signer)
		if err != nil {
			return ctx, err
		}
		if !found {
			continue
		}

		// step 2: expiry / revocation
		if m.Revoked || uint64(ctx.BlockHeight()) >= m.ExpiryHeight {
			return ctx, d.deny(ctx, m, errorsmod.Wrapf(types.ErrMandateExpired,
				"mandate %s: revoked=%t expiry_height=%d height=%d",
				m.Id, m.Revoked, m.ExpiryHeight, ctx.BlockHeight()))
		}

		// steps 3 + 4: recursive TypeURL allowlist walk and per-tx spend
		// intent, aggregated across ALL msgs in the tx (a tx of N
		// under-ceiling msgs whose sum exceeds the ceiling is rejected)
		allowed := make(map[string]bool, len(m.AllowedMsgTypes))
		for _, url := range m.AllowedMsgTypes {
			allowed[url] = true
		}
		// D-059: spend splits into a refundable intent COMMITMENT (lifetime
		// outstanding) and non-refundable SETTLED spend (per-period), tracked
		// separately by ChargeWindow. The split is computed per leaf msg and
		// summed across the tx (and through nested wrappers).
		commit := sdk.NewCoins()
		settled := sdk.NewCoins()
		for _, msg := range tx.GetMsgs() {
			msgCommit, msgSettled, err := d.checkMsg(ctx, allowed, msg, 0, params.MaxMsgNestingDepth)
			if err != nil {
				return ctx, d.deny(ctx, m, err)
			}
			commit = commit.Add(msgCommit...)
			settled = settled.Add(msgSettled...)
		}

		// steps 4 + 5: ceilings + optimistic charge (CheckTx AND DeliverTx —
		// see Keeper.ChargeWindow)
		if err := d.keeper.ChargeWindow(ctx, m, commit, settled); err != nil {
			return ctx, d.deny(ctx, m, err)
		}

		// step 6: counterparty-class predicates are NOT checked in ante —
		// the counterparty is unknown pre-match; x/session enforces them at
		// solution validation (SPEC §6.2 step 6, §6.4 rule 5, task 0.8)
	}

	return next(ctx, tx, simulate)
}

// checkMsg implements SPEC §6.2 step 3 (TypeURL allowlist with recursive
// nested-msg unwrap, recursion capped at max_msg_nesting_depth) and step 4
// (spend intent valuation), in one walk. depth counts wrapper levels above
// this msg: top-level msgs are depth 0; depth > maxDepth → ErrMsgTooNested.
func (d MandateDecorator) checkMsg(ctx sdk.Context, allowed map[string]bool, msg sdk.Msg, depth, maxDepth uint32) (commit, settled sdk.Coins, err error) {
	if depth > maxDepth {
		return nil, nil, errorsmod.Wrapf(types.ErrMsgTooNested,
			"nested msg at depth %d exceeds max_msg_nesting_depth %d", depth, maxDepth)
	}

	url := sdk.MsgTypeURL(msg)
	if !allowed[url] {
		return nil, nil, errorsmod.Wrapf(types.ErrMsgNotPermitted, "%s", url)
	}

	commit, settled = d.spendIntent(ctx, msg)

	// nested msgs matter: a wrapper's inner msgs are checked against the
	// same allowlist and their spend intents charged to the same mandate —
	// wrapping can never widen a mandate or dodge a ceiling
	for _, innerAny := range nestedAnys(msg) {
		var inner sdk.Msg
		if err := d.keeper.Codec().UnpackAny(innerAny, &inner); err != nil {
			return nil, nil, errorsmod.Wrapf(types.ErrMsgNotPermitted,
				"undecodable nested msg %s: %s", innerAny.TypeUrl, err)
		}
		innerCommit, innerSettled, err := d.checkMsg(ctx, allowed, inner, depth+1, maxDepth)
		if err != nil {
			return nil, nil, err
		}
		commit = commit.Add(innerCommit...)
		settled = settled.Add(innerSettled...)
	}
	return commit, settled, nil
}

// nestedAnys returns the packed inner msgs of known wrapper msg types
// (SPEC §6.2 step 3: "MsgExec (authz) and any wrapper"). Any new wrapper
// wired into the app MUST be added here; until it is, the allowlist still
// protects by default because controllers cannot meaningfully permit a
// wrapper this walk does not unwrap.
func nestedAnys(msg sdk.Msg) []*codectypes.Any {
	switch w := msg.(type) {
	case *authz.MsgExec:
		return w.Msgs
	case *govv1.MsgSubmitProposal:
		return w.Messages
	default:
		return nil
	}
}

// spendIntent implements SPEC §6.2 step 4 valuation for one msg, classified by
// D-059 into (commit, settled): a MsgSubmitIntent is a REFUNDABLE commitment
// (the per-denom max of give legs + bond, released when the intent terminates);
// bank sends are NON-REFUNDABLE settled spend (the send amount). Unknown msg
// types carry zero spend intent in both buckets: they are gated by the step 3
// allowlist instead.
func (d MandateDecorator) spendIntent(ctx sdk.Context, msg sdk.Msg) (commit, settled sdk.Coins) {
	switch m := msg.(type) {
	case *banktypes.MsgSend:
		return sdk.NewCoins(), sdk.NewCoins(m.Amount...)
	case *banktypes.MsgMultiSend:
		total := sdk.NewCoins()
		for _, input := range m.Inputs {
			total = total.Add(input.Coins...)
		}
		return sdk.NewCoins(), total
	case *veiltypes.MsgSubmitIntent:
		return d.intentSpend(ctx, m), sdk.NewCoins()
	default:
		return sdk.NewCoins(), sdk.NewCoins()
	}
}

// intentSpend values a MsgSubmitIntent as the declared spend_commitment
// PLUS the intent bond (D-025/D-065). The ante reads the agent's plaintext
// spend_commitment from the envelope and NEVER reads give legs — uniformly
// in stub and encrypted mode. This is the load-bearing pre-crypto property:
// once payloads are opaque (Task 1.6) the ante cannot value give legs, so
// binding the ceilings to the commitment is the only way the §6.2
// chokepoint stays sighted; building it in stub mode proves the exact path
// that ships, against plaintext the Reveal can still cross-check (D-065).
//
// The bond counts because it is slashable — 50% on INVALID (§6.3) — so
// unbounded bond spend would be an unbounded burn for a stolen key, the
// exact threat Mandates cap. Consequence: a mandate permitting
// MsgSubmitIntent must list a native-denom ceiling covering bonds (D-019
// deny-by-default). An under-declared commitment passes the ceiling here
// but is slashed at the Reveal (actual give > commitment → INVALID), so the
// declaration is truthful by deterrent, not by an ante cross-check it
// cannot perform on a ciphertext.
func (d MandateDecorator) intentSpend(_ sdk.Context, msg *veiltypes.MsgSubmitIntent) sdk.Coins {
	spend := spendCommitment(msg.Envelope)
	if !msg.Bond.IsNil() && msg.Bond.IsValid() && msg.Bond.IsPositive() {
		spend = spend.Add(msg.Bond)
	}
	return spend
}

// spendCommitment reads the plaintext spend_commitment from a MsgSubmitIntent
// envelope, behind recover so a malformed envelope can never panic the ante
// (SPEC §11.6). A malformed/undecodable envelope contributes a zero
// commitment here (its bond still charges); the msg server rejects it at
// execution via ValidateEnvelope, so it never reaches the Veil.
func spendCommitment(envelope []byte) (commitment sdk.Coins) {
	defer func() {
		if r := recover(); r != nil {
			commitment = sdk.NewCoins()
		}
	}()

	var env veiltypes.IntentEnvelope
	if err := env.Unmarshal(envelope); err != nil {
		return sdk.NewCoins()
	}
	return env.SpendCommitment
}

// deny emits the SPEC §6.2 mandate_enforced event in CheckTx for operator
// observability before returning the rejection.
func (d MandateDecorator) deny(ctx sdk.Context, m types.Mandate, err error) error {
	if ctx.IsCheckTx() {
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeMandateEnforced,
			sdk.NewAttribute(types.AttributeKeyMandateID, m.Id),
			sdk.NewAttribute(types.AttributeKeyGrantee, m.GranteeAddr),
			sdk.NewAttribute(types.AttributeKeyReason, err.Error()),
		))
		ctx.Logger().Info("mandate enforced",
			"mandate_id", m.Id, "grantee", m.GranteeAddr, "reason", err.Error())
	}
	return err
}
