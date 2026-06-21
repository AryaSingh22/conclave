package keeper

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/crypto/reveal"
	"github.com/AryaSingh22/conclave/x/veil/types"
)

// RevealTarget is one in-flight (SEALED_IN_SESSION) ciphertext awaiting the
// Reveal, tagged to a DKG epoch (Task 1.6). It is the unit ExtendVote produces
// a partial-decryption share for and BeginBlock(B+1) combines. Only intents with
// a non-zero epoch_id appear: epoch_id==0 is the StubScheme path (identity
// Reveal, no shares), so it never needs vote-extension transport.
type RevealTarget struct {
	IntentID   string
	SessionID  uint64
	Ciphertext []byte
	EpochID    uint64
}

// EnumerateInFlightForReveal returns every SEALED_IN_SESSION intent tagged to a
// DKG epoch (epoch_id != 0), in the deterministic §6.3 0x02 state/ttl index
// order. This is THE CONTRACT between ordering and share content (PHASE1_DESIGN
// §2): every validator's ExtendVote covers the identical ciphertext set because
// they all walk the same committed index, and BeginBlock combines over the same
// set. It is randomness-free and reads only committed state, so it is
// deterministic across nodes (SPEC §11.6).
//
// Because ExtendVote(h) runs BEFORE FinalizeBlock(h) (ABCI++), the committed
// view it reads covers everything sealed through block h-1; an intent sealed at
// boundary B is first enumerated at height B+1. The order-then-reveal guarantee
// rests on exactly this: a valid share for a ciphertext cannot be produced until
// the seal that fixed its ordering is in COMMITTED state.
func (k Keeper) EnumerateInFlightForReveal(ctx sdk.Context) ([]RevealTarget, error) {
	ids, err := k.intentIDsInState(ctx, types.INTENT_STATE_SEALED_IN_SESSION, types.MaxTTL)
	if err != nil {
		return nil, err
	}
	out := make([]RevealTarget, 0, len(ids))
	for _, id := range ids {
		rec, err := k.GetIntentRecord(ctx, phase0Chamber, id)
		if err != nil {
			return nil, err
		}
		var env types.IntentEnvelope
		if err := env.Unmarshal(rec.Intent.Envelope); err != nil {
			// stored envelopes are validated at submission; a decode failure here
			// is state corruption — skip it rather than poison the whole reveal
			// set (it cannot be revealed, and will drain via retry→CANCEL).
			continue
		}
		if env.EpochId == 0 {
			continue
		}
		out = append(out, RevealTarget{
			IntentID:   id,
			SessionID:  rec.SessionId,
			Ciphertext: rec.Intent.Ciphertext,
			EpochID:    env.EpochId,
		})
	}
	return out, nil
}

// RevealShareSource is the per-block supply of collected vote-extension reveal
// shares the Reveal consumes (Task 1.6). It is populated by the app's PreBlocker
// from the proposer-injected, ValidateVoteExtensions-verified ExtendedCommitInfo
// of the prior block, and read by RevealSealed at the matching height. The veil
// keeper depends on this interface (not the app's concrete cache) so the keeper
// stays free of ABCI/consensus types; nil ⇒ no transport wired (stub/unit-test
// mode), and the Reveal sees no shares (every epoch_id!=0 intent retries).
type RevealShareSource interface {
	// SharesFor returns the reveal shares collected for an intent at a height.
	SharesFor(height int64, intentID string) []reveal.RevealShare
}

// SetRevealShareSource wires the per-block share supply after construction
// (mirrors SetEpochKeeper). App wiring MUST call this before the veil AppModule
// is captured by RegisterModules so the BeginBlocker copy observes it.
func (k *Keeper) SetRevealShareSource(s RevealShareSource) { k.revealShares = s }

// --- per-session reveal-attempt counter (Task 1.6 retry→CANCEL, §2(c)) ---

// getRevealAttempts returns the recorded reveal-attempt count for a session.
func (k Keeper) getRevealAttempts(ctx sdk.Context, sessionID uint64) uint32 {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.RevealAttemptsKey(sessionID))
	if err != nil || bz == nil {
		return 0
	}
	return binary.BigEndian.Uint32(bz)
}

// setRevealAttempts stores the reveal-attempt count for a session.
func (k Keeper) setRevealAttempts(ctx sdk.Context, sessionID uint64, n uint32) {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, n)
	if err := k.storeService.OpenKVStore(ctx).Set(types.RevealAttemptsKey(sessionID), out); err != nil {
		panic(err)
	}
}

// deleteRevealAttempts clears a session's reveal-attempt counter (on CANCEL or
// once the session has fully drained).
func (k Keeper) deleteRevealAttempts(ctx sdk.Context, sessionID uint64) {
	if err := k.storeService.OpenKVStore(ctx).Delete(types.RevealAttemptsKey(sessionID)); err != nil {
		panic(err)
	}
}

// revealAttemptSessions returns every session id with a stored reveal-attempt
// counter, in ascending order (deterministic — used to clear stale counters).
func (k Keeper) revealAttemptSessions(ctx sdk.Context) []uint64 {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.RevealAttemptsKeyPrefix, storePrefixEndVeil(types.RevealAttemptsKeyPrefix))
	if err != nil {
		panic(err)
	}
	defer it.Close()
	var out []uint64
	for ; it.Valid(); it.Next() {
		out = append(out, binary.BigEndian.Uint64(it.Key()[len(types.RevealAttemptsKeyPrefix):]))
	}
	return out
}

// storePrefixEndVeil returns the exclusive end key for a single-byte prefix scan.
func storePrefixEndVeil(prefix []byte) []byte {
	end := make([]byte, len(prefix))
	copy(end, prefix)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end[:i+1]
		}
	}
	return nil
}
