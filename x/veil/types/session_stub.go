package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultSessionLengthBlocks is the dev session cadence (SPEC §6.4 params:
// session_length_blocks, dev default 3; SPEC §14 D1).
const DefaultSessionLengthBlocks = 3

// StubSessionKeeper is the Phase-0 SessionKeeper (D-038): the session
// boundary fires every SessionLengthBlocks blocks, and the session id is
// the boundary block height — deterministic, unique per boundary, and it
// makes the order-then-reveal invariant directly checkable (the Reveal in
// BeginBlock(H) only touches records with session_id < H, i.e. ordering
// fixed in a strictly earlier block).
//
// SessionLengthBlocks is constructor-fixed (not a veil param): the
// governable session_length_blocks param belongs to x/session per SPEC
// §6.4 — parking it in veil params would have created a param-migration
// conflict.
//
// MARKER RETIRED at Rev 3 (D-090): the former phase-0 marker said
// "task 0.8 replaces this with the real x/session keeper in app wiring", and
// 0.8 did — app.go wires the real keeper and this type has ZERO non-test
// callers. It is kept as a test double, not as a live path, which is why the
// marker was stale rather than pending (AUDIT F-8's class).
type StubSessionKeeper struct {
	SessionLengthBlocks uint64
}

var _ SessionKeeper = StubSessionKeeper{}

// NewStubSessionKeeper creates the stub with the given cadence (0 falls
// back to the dev default).
func NewStubSessionKeeper(sessionLengthBlocks uint64) StubSessionKeeper {
	if sessionLengthBlocks == 0 {
		sessionLengthBlocks = DefaultSessionLengthBlocks
	}
	return StubSessionKeeper{SessionLengthBlocks: sessionLengthBlocks}
}

// SessionBoundary implements SessionKeeper: ordering fixes whenever the
// height is a multiple of the cadence; the session id is the boundary
// height (D-038).
func (s StubSessionKeeper) SessionBoundary(ctx sdk.Context) (bool, uint64) {
	height := uint64(ctx.BlockHeight())
	if height == 0 || height%s.SessionLengthBlocks != 0 {
		return false, 0
	}
	return true, height
}

// IntakeRevealed implements SessionKeeper as a no-op: there is no session
// state machine until task 0.8.
func (s StubSessionKeeper) IntakeRevealed(_ sdk.Context, _ uint64, _ []string) error {
	return nil
}
