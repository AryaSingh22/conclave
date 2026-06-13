package keeper

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/veil/types"
)

// GetIntentRecord loads an intent record by hex id. Implements SPEC §5.3
// key 0x01|chamber|intent_id.
func (k Keeper) GetIntentRecord(ctx sdk.Context, chamberID uint32, id string) (types.IntentRecord, error) {
	idBz, err := types.IntentIDBytes(id)
	if err != nil {
		return types.IntentRecord{}, err
	}
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.IntentKey(chamberID, idBz))
	if err != nil {
		return types.IntentRecord{}, err
	}
	if bz == nil {
		return types.IntentRecord{}, errorsmod.Wrapf(types.ErrIntentNotFound, "id %s", id)
	}
	var rec types.IntentRecord
	if err := k.cdc.Unmarshal(bz, &rec); err != nil {
		return types.IntentRecord{}, err
	}
	return rec, nil
}

// setIntentRecord persists a record under 0x01 and (re)writes its 0x02
// state index entry; oldState's entry is removed when the state changed.
// All record writes flow through here so the state index can never drift
// from the records (SPEC §5.2 "index by (state, ttl_height)").
func (k Keeper) setIntentRecord(ctx sdk.Context, rec types.IntentRecord, oldState types.IntentState) error {
	idBz, err := types.IntentIDBytes(rec.Intent.Id)
	if err != nil {
		return err
	}
	ttl, err := k.recordTTL(rec)
	if err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)

	bz, err := k.cdc.Marshal(&rec)
	if err != nil {
		return err
	}
	if err := store.Set(types.IntentKey(rec.Intent.ChamberId, idBz), bz); err != nil {
		return err
	}
	if oldState != rec.State && oldState != types.INTENT_STATE_UNSPECIFIED {
		if err := store.Delete(types.StateIndexKey(oldState, ttl, idBz)); err != nil {
			return err
		}
	}
	return store.Set(types.StateIndexKey(rec.State, ttl, idBz), []byte{})
}

// recordTTL extracts the envelope ttl_height the 0x02 index is keyed by.
// The envelope was validated at submission (and at genesis import), so a
// decode failure here is state corruption, not user input.
func (k Keeper) recordTTL(rec types.IntentRecord) (uint64, error) {
	var env types.IntentEnvelope
	if err := env.Unmarshal(rec.Intent.Envelope); err != nil {
		return 0, errorsmod.Wrapf(types.ErrInvalidEnvelope,
			"stored envelope of intent %s undecodable: %s", rec.Intent.Id, err)
	}
	return env.TtlHeight, nil
}

// transitionIntent is the single chokepoint for every lifecycle state
// change (SPEC §5.2; §11.2 "illegal transitions are impossible"): it
// validates the edge against types.LegalTransitions, then persists the
// record and moves its index entry. No other code path writes
// IntentRecord.state.
func (k Keeper) transitionIntent(ctx sdk.Context, rec *types.IntentRecord, to types.IntentState) error {
	from := rec.State
	if !types.CanTransition(from, to) {
		return errorsmod.Wrapf(types.ErrIllegalTransition, "%s -> %s", from, to)
	}
	rec.State = to
	return k.setIntentRecord(ctx, *rec, from)
}

// intentIDsInState returns the hex ids of every intent currently indexed in
// one state whose ttl_height <= maxTTL (use types.MaxTTL for "all"),
// in index order (ttl ascending, then id) — a deterministic, sorted order
// safe to drive state writes (SPEC §11.6).
func (k Keeper) intentIDsInState(ctx sdk.Context, state types.IntentState, maxTTL uint64) ([]string, error) {
	store := k.storeService.OpenKVStore(ctx)
	start := types.StateIndexIterPrefix(state)
	end := types.StateIndexExpiryEnd(state, maxTTL)

	it, err := store.Iterator(start, end)
	if err != nil {
		return nil, err
	}
	defer it.Close()

	var ids []string
	for ; it.Valid(); it.Next() {
		idBz := types.IntentIDFromStateIndexKey(it.Key())
		ids = append(ids, types.IntentIDHex(idBz))
	}
	return ids, nil
}

// IterateIntentRecords walks every stored intent record in key order
// (genesis export, queries, invariants).
func (k Keeper) IterateIntentRecords(ctx sdk.Context, fn func(rec types.IntentRecord) bool) error {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.IntentKeyPrefix, prefixEnd(types.IntentKeyPrefix))
	if err != nil {
		return err
	}
	defer it.Close()

	for ; it.Valid(); it.Next() {
		var rec types.IntentRecord
		if err := k.cdc.Unmarshal(it.Value(), &rec); err != nil {
			return err
		}
		if fn(rec) {
			return nil
		}
	}
	return nil
}

// prefixEnd returns the exclusive upper bound for iterating a key prefix.
func prefixEnd(prefix []byte) []byte {
	end := make([]byte, len(prefix))
	copy(end, prefix)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] != 0xFF {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}
