package keeper

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// legalSessionTransitions encodes the SPEC §6.4 session lifecycle:
//
//	FORMING → AUCTION                  (intents revealed; auction opens)
//	AUCTION → SCORING | FALLBACK | CLOSED
//	SCORING → CONCLUDING | FALLBACK
//	CONCLUDING → CLOSED | FALLBACK     (a leg failure falls through to FALLBACK)
//	FALLBACK → CLOSED
//
// CLOSED is terminal. Every session state change routes through
// transitionSession so illegal transitions are impossible (§11.2). FORMING is
// the initial state of a carryover session pre-created by the rollover sweep
// (Half B); in the stub-cadence happy path IntakeRevealed creates sessions
// directly in AUCTION (the pre-reveal "forming" is x/veil's SUBMITTED/SEALED
// accumulation).
var legalSessionTransitions = map[types.SessionState][]types.SessionState{
	types.SESSION_STATE_FORMING: {
		types.SESSION_STATE_AUCTION,
		types.SESSION_STATE_CLOSED,
	},
	types.SESSION_STATE_AUCTION: {
		types.SESSION_STATE_SCORING,
		types.SESSION_STATE_FALLBACK,
		types.SESSION_STATE_CLOSED,
	},
	types.SESSION_STATE_SCORING: {
		types.SESSION_STATE_CONCLUDING,
		types.SESSION_STATE_FALLBACK,
		types.SESSION_STATE_CLOSED,
	},
	types.SESSION_STATE_CONCLUDING: {
		types.SESSION_STATE_CLOSED,
		types.SESSION_STATE_FALLBACK,
	},
	types.SESSION_STATE_FALLBACK: {
		types.SESSION_STATE_CLOSED,
	},
}

// canTransitionSession reports whether from → to is a legal §6.4 transition.
func canTransitionSession(from, to types.SessionState) bool {
	for _, legal := range legalSessionTransitions[from] {
		if legal == to {
			return true
		}
	}
	return false
}

// GetSession loads a session by id.
func (k Keeper) GetSession(ctx sdk.Context, chamberID uint32, id uint64) (types.Session, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.SessionKey(chamberID, id))
	if err != nil {
		return types.Session{}, err
	}
	if bz == nil {
		return types.Session{}, errorsmod.Wrapf(types.ErrSessionNotFound, "id %d", id)
	}
	var s types.Session
	if err := k.cdc.Unmarshal(bz, &s); err != nil {
		return types.Session{}, err
	}
	return s, nil
}

// setSession persists a session and (re)writes its state index entry; the old
// state's index entry is removed when the state changed, so the index can
// never drift from the records.
func (k Keeper) setSession(ctx sdk.Context, s types.Session, oldState types.SessionState) error {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := k.cdc.Marshal(&s)
	if err != nil {
		return err
	}
	if err := store.Set(types.SessionKey(s.ChamberId, s.Id), bz); err != nil {
		return err
	}
	if oldState != s.State && oldState != types.SESSION_STATE_UNSPECIFIED {
		if err := store.Delete(types.SessionStateIdxKey(oldState, s.Id)); err != nil {
			return err
		}
	}
	return store.Set(types.SessionStateIdxKey(s.State, s.Id), []byte{})
}

// transitionSession is the single chokepoint for session state changes
// (SPEC §6.4; §11.2 illegal transitions impossible).
func (k Keeper) transitionSession(ctx sdk.Context, s *types.Session, to types.SessionState) error {
	from := s.State
	if !canTransitionSession(from, to) {
		return errorsmod.Wrapf(types.ErrInvalidSolution, "illegal session transition %s -> %s", from, to)
	}
	s.State = to
	return k.setSession(ctx, *s, from)
}

// SessionBoundary implements veiltypes.SessionKeeper (SPEC §6.3 ABCI hooks):
// intent ordering fixes whenever the height is a multiple of the governable
// session_length_blocks, and the session id is the boundary height. Keeping
// the D-038 session-id == boundary-height scheme leaves x/veil's
// order-then-reveal check (session_id < height) unchanged — only the cadence
// source moves from a veil constructor constant to this module's §6.4 param.
func (k Keeper) SessionBoundary(ctx sdk.Context) (bool, uint64) {
	height := uint64(ctx.BlockHeight())
	params, err := k.GetParams(ctx)
	if err != nil || params.SessionLengthBlocks == 0 {
		return false, 0
	}
	if height == 0 || height%params.SessionLengthBlocks != 0 {
		return false, 0
	}
	return true, height
}

// IntakeRevealed implements veiltypes.SessionKeeper (SPEC §6.4 EndBlock step 2
// "Reveal hand-off intake"), called from x/veil's BeginBlock Reveal once the
// sealed intents of a session have opened. It materializes the session in
// AUCTION with its fixed intent set and the auction window. A session
// pre-created in FORMING by the Half-B rollover sweep is advanced to AUCTION
// with the revealed ids appended to its carryover set.
func (k Keeper) IntakeRevealed(ctx sdk.Context, sessionID uint64, intentIDs []string) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	height := uint64(ctx.BlockHeight())
	auctionEnd := height + params.SolutionWindowBlocks - 1

	existing, err := k.GetSession(ctx, phase0Chamber, sessionID)
	switch {
	case err == nil:
		// carryover session pre-created in FORMING (Half B): append revealed
		// intents and open the auction.
		existing.IntentIds = append(existing.IntentIds, intentIDs...)
		existing.AuctionEndHeight = auctionEnd
		if existing.State == types.SESSION_STATE_FORMING {
			return k.transitionSession(ctx, &existing, types.SESSION_STATE_AUCTION)
		}
		return k.setSession(ctx, existing, existing.State)
	case errorsmod.IsOf(err, types.ErrSessionNotFound):
		s := types.Session{
			Id:               sessionID,
			ChamberId:        phase0Chamber,
			State:            types.SESSION_STATE_AUCTION,
			StartHeight:      height,
			AuctionEndHeight: auctionEnd,
			IntentIds:        intentIDs,
		}
		return k.setSession(ctx, s, types.SESSION_STATE_UNSPECIFIED)
	default:
		return err
	}
}

// CurrentSession returns the most recent non-terminal session of a chamber
// (the one solvers should read, SPEC §7). Falls back to the highest-id
// session if all are CLOSED.
func (k Keeper) CurrentSession(ctx sdk.Context, chamberID uint32) (types.Session, error) {
	store := k.storeService.OpenKVStore(ctx)
	prefix := types.SessionChamberPrefix(chamberID)
	it, err := store.Iterator(prefix, types.PrefixEnd(prefix))
	if err != nil {
		return types.Session{}, err
	}
	defer it.Close()

	// keys sort ascending by id; the last non-terminal session is the current
	// one (what solvers should read), else the highest-id session overall.
	var highest, nonTerminal types.Session
	var anyFound, ntFound bool
	for ; it.Valid(); it.Next() {
		var s types.Session
		if err := k.cdc.Unmarshal(it.Value(), &s); err != nil {
			return types.Session{}, err
		}
		highest = s
		anyFound = true
		if s.State != types.SESSION_STATE_CLOSED {
			nonTerminal = s
			ntFound = true
		}
	}
	switch {
	case ntFound:
		return nonTerminal, nil
	case anyFound:
		return highest, nil
	default:
		return types.Session{}, errorsmod.Wrap(types.ErrSessionNotFound, "no sessions")
	}
}

// IterateSessions walks every session in key order (genesis export, queries).
func (k Keeper) IterateSessions(ctx sdk.Context, fn func(s types.Session) bool) error {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.SessionKeyPrefix, types.PrefixEnd(types.SessionKeyPrefix))
	if err != nil {
		return err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var s types.Session
		if err := k.cdc.Unmarshal(it.Value(), &s); err != nil {
			return err
		}
		if fn(s) {
			return nil
		}
	}
	return nil
}
