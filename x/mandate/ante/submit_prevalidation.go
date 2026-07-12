package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	"github.com/AryaSingh22/conclave/x/mandate/keeper"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// SubmitIntentValidator is the x/veil surface the prevalidation decorator
// depends on: the payload-independent preconditions of MsgSubmitIntent
// (implemented by x/veil keeper.ValidateSubmitIntentPreconditions). ONE
// method, ONE source of truth — the veil msg server runs the SAME function,
// so the ante and the msg server cannot diverge: a new payload-independent
// precondition added to the msg server is added to this function, hence to
// both callers, so no new strand can open silently (D-085/G2).
type SubmitIntentValidator interface {
	ValidateSubmitIntentPreconditions(ctx sdk.Context, msg *veiltypes.MsgSubmitIntent) (veiltypes.IntentEnvelope, sdk.AccAddress, error)
}

// SubmitIntentPrevalidationDecorator rejects, BEFORE the MandateDecorator
// window charge, any MsgSubmitIntent that would fail the x/veil msg server's
// payload-independent preconditions (D-085/G2 mitigation).
//
// Why it exists: baseapp commits the ante's optimistic mandate charge (SPEC
// §6.2 step 5) to the delivery state BEFORE runMsgs, and discards the msg
// cache on failure with no rollback of the ante writes. So a MsgSubmitIntent
// that passes the ante (sig/mandate/ceiling) but fails in the veil msg server
// (bad sigil, under-min bond, malformed envelope, undecryptable epoch, wrong
// chamber, unfundable bond) permanently strands its charge in
// outstanding_commitment — a permissionless, on-demand break of the
// D-059/D-064 conservation invariant. Pre-rejecting those exact failure modes
// closes it: no ante-passing submit can fail in the msg server, so nothing
// strands.
//
// It walks nested msgs EXACTLY as MandateDecorator does (the same nestedAnys
// unwrap and the same max_msg_nesting_depth bound), so it covers every
// MsgSubmitIntent the mandate would charge — including ones wrapped in
// MsgExec / MsgSubmitProposal. It runs after signature verification and
// before the charge (placed immediately ahead of MandateDecorator).
type SubmitIntentPrevalidationDecorator struct {
	keeper    keeper.Keeper
	validator SubmitIntentValidator
}

// NewSubmitIntentPrevalidationDecorator creates the D-085/G2 mitigation
// decorator. keeper is the x/mandate keeper (for the codec that unpacks
// nested msgs and the max_msg_nesting_depth param); validator is the x/veil
// keeper.
func NewSubmitIntentPrevalidationDecorator(k keeper.Keeper, v SubmitIntentValidator) SubmitIntentPrevalidationDecorator {
	return SubmitIntentPrevalidationDecorator{keeper: k, validator: v}
}

var _ sdk.AnteDecorator = SubmitIntentPrevalidationDecorator{}

// AnteHandle validates every MsgSubmitIntent in a mandate-charged tx
// (top-level and nested) against the veil preconditions; the first failure
// rejects the whole tx before the mandate charge. Runs identically in CheckTx
// and DeliverTx.
//
// SCOPE — only txs whose charge can strand: MandateDecorator charges a submit
// only when a tx signer is mandate-bound, so a submit from a root/controller
// key is never charged and a msg-server failure there cannot strand. Skipping
// those keeps the guard to EXACTLY what the conservation break requires
// (mirroring MandateDecorator's own signer walk) and leaves root-key submit
// behavior — and the determinism vector — unchanged.
func (d SubmitIntentPrevalidationDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return next(ctx, tx, simulate)
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}
	charged := false
	for _, signerBz := range signers {
		if _, found, err := d.keeper.ActiveMandateByGrantee(ctx, keeper.Phase0Chamber(), sdk.AccAddress(signerBz)); err != nil {
			return ctx, err
		} else if found {
			charged = true
			break
		}
	}
	if !charged {
		return next(ctx, tx, simulate)
	}

	params, err := d.keeper.GetParams(ctx)
	if err != nil {
		return ctx, err
	}
	for _, msg := range tx.GetMsgs() {
		if err := d.prevalidate(ctx, msg, 0, params.MaxMsgNestingDepth); err != nil {
			return ctx, err
		}
	}
	return next(ctx, tx, simulate)
}

// prevalidate mirrors MandateDecorator.checkMsg's walk — same nestedAnys, same
// depth bound — but validates only MsgSubmitIntent preconditions (the msgs the
// mandate charges). A tx nested deeper than maxDepth is rejected by
// MandateDecorator with ErrMsgTooNested before any charge, so we simply stop
// descending at the bound: nothing beyond it is ever charged, hence nothing
// beyond it can strand.
func (d SubmitIntentPrevalidationDecorator) prevalidate(ctx sdk.Context, msg sdk.Msg, depth, maxDepth uint32) error {
	if depth > maxDepth {
		return nil
	}
	if sub, ok := msg.(*veiltypes.MsgSubmitIntent); ok {
		if _, _, err := d.validator.ValidateSubmitIntentPreconditions(ctx, sub); err != nil {
			return err
		}
	}
	for _, innerAny := range nestedAnys(msg) {
		var inner sdk.Msg
		if err := d.keeper.Codec().UnpackAny(innerAny, &inner); err != nil {
			// an undecodable nested msg is rejected by MandateDecorator's own
			// unpack (ErrMsgNotPermitted) before it charges, so it never
			// strands — let it fall through to that rejection rather than
			// erroring here with a different code.
			continue
		}
		if err := d.prevalidate(ctx, inner, depth+1, maxDepth); err != nil {
			return err
		}
	}
	return nil
}
