package ante

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/mandate/keeper"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
)

// ProposalSlasher is the x/session surface this decorator depends on: ONE
// method, which applies the SPEC §6.4 proposal-time bond slash for a proposal
// the session msg server is about to reject (implemented by x/session
// keeper.ApplyProposalSlash). It shares its verdict with the msg server
// through keeper.EvaluateProposal — one implementation, two callers, so the
// deterrent and the rejection cannot drift (D-085's rule, D-097's defect).
type ProposalSlasher interface {
	ApplyProposalSlash(ctx sdk.Context, solver sdk.AccAddress, sessionID uint64, matches []sessiontypes.Match) error
}

// ProposeSolutionSlashDecorator applies the SPEC §6.4 bond slashes for an
// invalid solution (5%) and equivocation (20%).
//
// WHY THE SLASH LIVES IN THE ANTE (task 2.17, D-097 finding F-1). x/session
// used to slash inside its msg server and then return the rejection error.
// cosmos-sdk v0.50 branches the store twice per tx: the ante's branch commits
// the moment the ante returns nil (baseapp.go:933), before any message runs;
// the message branch commits only when every message succeeds
// (baseapp.go:989). So the slash was written into the branch the rejection
// threw away, and a rejected proposal cost a solver GAS ONLY — the deterrent
// the untrusted-solver model rests on did not exist. The ante is not a
// convenient place to put this; it is the only place in v0.50 where the write
// survives the message it is a penalty for.
//
// It does NOT reject the proposal. Rejection stays in the msg server, so every
// wire identity task 2.13 established is unchanged: an invalid solution still
// arrives as ("session", 1412) with the specific rule reachable by errors.Is,
// and equivocation as ("session", 1410). This decorator returns nil for a
// slashable proposal precisely SO THAT the ante's branch commits and the
// message can then fail.
//
// It is placed LAST in the chain. Correctness does not require that — a later
// ante failure would roll the slash back, which is right, since the message
// would never run and no penalty is owed — but placing it last means nothing
// downstream can silently undo the one write in the ante chain that is
// supposed to outlive its message.
type ProposeSolutionSlashDecorator struct {
	keeper  keeper.Keeper
	slasher ProposalSlasher
}

// NewProposeSolutionSlashDecorator creates the task-2.17 decorator. k is the
// x/mandate keeper (for the codec that unpacks nested msgs and the
// max_msg_nesting_depth bound, exactly as SubmitIntentPrevalidationDecorator
// uses it); slasher is the x/session keeper.
func NewProposeSolutionSlashDecorator(k keeper.Keeper, slasher ProposalSlasher) ProposeSolutionSlashDecorator {
	return ProposeSolutionSlashDecorator{keeper: k, slasher: slasher}
}

var _ sdk.AnteDecorator = ProposeSolutionSlashDecorator{}

// AnteHandle enforces the sole-message rule and applies the slash.
//
// THE SOLE-MESSAGE RULE (SPEC §6.4 as implemented; ErrProposalNotSole, session
// 1430). A MsgProposeSolution must be the ONLY top-level message of its tx and
// must not be nested in a wrapper. The ante evaluates the proposal against
// pre-tx state; the msg server evaluates it against state that includes every
// earlier message in the same tx. Without this rule those two verdicts can
// disagree, and both directions are defects: a sibling message that turns an
// invalid proposal VALID slashes a solver whose solution then succeeds, and
// one that turns a valid proposal INVALID is a deliberate way to propose
// garbage for free. With it, both evaluations read the same state and run the
// same function, so their agreement is a property of the code rather than a
// hope — the guarantee does not depend on the shape of the attacker's tx.
//
// Runs identically in CheckTx, DeliverTx and simulation: no exec-mode branch
// (D-059's "zero mode branching" posture). A CheckTx-side write lands in the
// mempool's own branch and is reset at the next Commit; simulation discards
// everything it writes, and running the same code there keeps gas estimation
// honest about what a rejected proposal will actually cost.
func (d ProposeSolutionSlashDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msgs := tx.GetMsgs()

	// The permitted shape: exactly one top-level message, and it is the
	// proposal itself.
	if len(msgs) == 1 {
		if msg, ok := msgs[0].(*sessiontypes.MsgProposeSolution); ok {
			addr, err := sdk.AccAddressFromBech32(msg.Solver)
			if err != nil {
				// Malformed solver address: no verdict is possible and the msg
				// server will reject it. Nothing to price.
				return next(ctx, tx, simulate)
			}
			if err := d.slasher.ApplyProposalSlash(ctx, addr, msg.SessionId, msg.Matches); err != nil {
				return ctx, err
			}
			return next(ctx, tx, simulate)
		}
	}

	// Any other shape: if a proposal is hiding anywhere in this tx — as a
	// sibling of other messages, or inside a wrapper the mandate walk unwraps
	// — reject it rather than let it execute unpriced.
	params, err := d.keeper.GetParams(ctx)
	if err != nil {
		return ctx, err
	}
	for _, msg := range msgs {
		if d.containsProposal(msg, 0, params.MaxMsgNestingDepth) {
			return ctx, errorsmod.Wrap(sessiontypes.ErrProposalNotSole,
				"a solution proposal must be submitted alone, in its own transaction")
		}
	}
	return next(ctx, tx, simulate)
}

// containsProposal reports whether msg is, or wraps, a MsgProposeSolution. It
// walks nested msgs EXACTLY as MandateDecorator and
// SubmitIntentPrevalidationDecorator do — the same nestedAnys unwrap and the
// same max_msg_nesting_depth bound — so a proposal cannot hide from the rule
// inside a wrapper that the rest of the ante chain does unwrap.
//
// A msg nested deeper than maxDepth is rejected by MandateDecorator with
// ErrMsgTooNested before any message executes, so stopping the descent at the
// bound leaves nothing reachable-but-unchecked.
func (d ProposeSolutionSlashDecorator) containsProposal(msg sdk.Msg, depth, maxDepth uint32) bool {
	if depth > maxDepth {
		return false
	}
	if _, ok := msg.(*sessiontypes.MsgProposeSolution); ok {
		return true
	}
	for _, innerAny := range nestedAnys(msg) {
		var inner sdk.Msg
		if err := d.keeper.Codec().UnpackAny(innerAny, &inner); err != nil {
			// An undecodable nested msg cannot execute either — MandateDecorator's
			// own unpack rejects it (ErrMsgNotPermitted). Let it fall through to
			// that rejection rather than claim a proposal is hiding in it.
			continue
		}
		if d.containsProposal(inner, depth+1, maxDepth) {
			return true
		}
	}
	return false
}
