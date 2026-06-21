// Package keeper implements x/epoch — the DKG epoch key lifecycle
// (PHASE1_DESIGN §2(b), D-068): deterministic epoch-boundary detection from the
// staking-finalized bonded set, the on-chain DKG message transport ("Candidate
// A": deals/responses/confirmations relayed as opaque bytes), EpochPubKey
// publication to queryable state, and drain-before-rotate. The module holds NO
// secret material — per-validator secret shares are node-local (D-062) — and no
// module account. The live x/veil Reveal stays on StubScheme until Task 1.6;
// x/epoch is the KEY-lifecycle surface 1.6 sources epoch material from.
package keeper

import (
	"encoding/binary"

	storetypes "cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/epoch/types"
)

// Keeper provides access to the x/epoch store. Returned as a pointer so
// SetVeilKeeper can wire the cyclic veil dependency in place (mirrors x/session).
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService storetypes.KVStoreService

	stakingKeeper types.StakingKeeper

	// veilKeeper is wired AFTER construction via SetVeilKeeper to break the
	// veil↔epoch cycle (veil needs IsDecryptableEpoch for submit-time epoch-tag
	// validation; epoch needs HasInFlightForEpoch for drain-before-rotate).
	veilKeeper types.VeilKeeper

	// authority is the address allowed to update params (gov module account).
	authority string
}

// NewKeeper creates a new x/epoch Keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	stakingKeeper types.StakingKeeper,
	authority string,
) *Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic("invalid x/epoch authority address: " + err.Error())
	}
	return &Keeper{
		cdc:           cdc,
		storeService:  storeService,
		stakingKeeper: stakingKeeper,
		authority:     authority,
	}
}

// SetVeilKeeper wires the x/veil keeper after both keepers exist. App
// construction MUST call this before any block runs; veil() panics otherwise.
func (k *Keeper) SetVeilKeeper(vk types.VeilKeeper) { k.veilKeeper = vk }

// veil returns the wired VeilKeeper, panicking if the cyclic dependency was
// never wired (it must be set during app construction, before any block).
func (k Keeper) veil() types.VeilKeeper {
	if k.veilKeeper == nil {
		panic("x/epoch: VeilKeeper is nil — app wiring must call SetVeilKeeper before any block")
	}
	return k.veilKeeper
}

// GetAuthority returns the module's authority address.
func (k Keeper) GetAuthority() string { return k.authority }

// GetParams returns the current module parameters.
func (k Keeper) GetParams(ctx sdk.Context) (types.Params, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.ParamsKey)
	if err != nil {
		return types.Params{}, err
	}
	if bz == nil {
		return types.DefaultParams(), nil
	}
	var p types.Params
	if err := k.cdc.Unmarshal(bz, &p); err != nil {
		return types.Params{}, err
	}
	return p, nil
}

// SetParams stores the module parameters.
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	bz, err := k.cdc.Marshal(&p)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(types.ParamsKey, bz)
}

// --- scalar id accessors (current ACTIVE / next / FORMING epoch ids) ---

func (k Keeper) getU64(ctx sdk.Context, key []byte) uint64 {
	bz, err := k.storeService.OpenKVStore(ctx).Get(key)
	if err != nil || bz == nil {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

func (k Keeper) setU64(ctx sdk.Context, key []byte, v uint64) {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	if err := k.storeService.OpenKVStore(ctx).Set(key, b); err != nil {
		panic(err)
	}
}

// CurrentEpochID is the ACTIVE epoch — the current encryption target (0 = none,
// the Task-1.6 bootstrap seam).
func (k Keeper) CurrentEpochID(ctx sdk.Context) uint64 { return k.getU64(ctx, types.CurrentEpochIDKey) }
func (k Keeper) setCurrentEpochID(ctx sdk.Context, id uint64) {
	k.setU64(ctx, types.CurrentEpochIDKey, id)
}

// NextEpochID is the id the next FORMING epoch will take (defaults to 1).
func (k Keeper) NextEpochID(ctx sdk.Context) uint64 {
	if id := k.getU64(ctx, types.NextEpochIDKey); id != 0 {
		return id
	}
	return 1
}
func (k Keeper) setNextEpochID(ctx sdk.Context, id uint64) { k.setU64(ctx, types.NextEpochIDKey, id) }

// FormingEpochID is the open FORMING epoch (0 = none).
func (k Keeper) FormingEpochID(ctx sdk.Context) uint64 { return k.getU64(ctx, types.FormingEpochIDKey) }
func (k Keeper) setFormingEpochID(ctx sdk.Context, id uint64) {
	k.setU64(ctx, types.FormingEpochIDKey, id)
}

// --- Epoch records ---

// GetEpoch returns an epoch record by id.
func (k Keeper) GetEpoch(ctx sdk.Context, id uint64) (types.Epoch, bool) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.EpochKey(id))
	if err != nil || bz == nil {
		return types.Epoch{}, false
	}
	var e types.Epoch
	if err := k.cdc.Unmarshal(bz, &e); err != nil {
		panic(err)
	}
	return e, true
}

// SetEpoch stores an epoch record.
func (k Keeper) SetEpoch(ctx sdk.Context, e types.Epoch) {
	bz, err := k.cdc.Marshal(&e)
	if err != nil {
		panic(err)
	}
	if err := k.storeService.OpenKVStore(ctx).Set(types.EpochKey(e.Id), bz); err != nil {
		panic(err)
	}
}

// IterateEpochs iterates every stored epoch record in ascending id order
// (deterministic). stop=true ends iteration.
func (k Keeper) IterateEpochs(ctx sdk.Context, fn func(types.Epoch) (stop bool)) {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.EpochPrefix, storePrefixEnd(types.EpochPrefix))
	if err != nil {
		panic(err)
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var e types.Epoch
		if err := k.cdc.Unmarshal(it.Value(), &e); err != nil {
			panic(err)
		}
		if fn(e) {
			return
		}
	}
}

// IsDecryptableEpoch reports whether epochID is currently decryptable — ACTIVE
// or DRAINING — i.e. an intent may legitimately be tagged to it (D-068). This is
// the both-edge gate x/veil's submit validation calls: a RETIRED/FAILED epoch
// (past edge) and a FORMING/unknown epoch with no published key (future edge)
// both return false. epochID==0 (stub/unset) is handled by the caller.
func (k Keeper) IsDecryptableEpoch(ctx sdk.Context, epochID uint64) bool {
	e, ok := k.GetEpoch(ctx, epochID)
	if !ok {
		return false
	}
	return e.Status == types.EPOCH_STATUS_ACTIVE || e.Status == types.EPOCH_STATUS_DRAINING
}

// CommitteeSize returns the DKG committee size (n) of an epoch — the validator
// count its threshold scheme was built for. Used by the Task-1.6 vote-extension
// verification to range-check a share's dkg_index. ok=false if the epoch is
// unknown.
func (k Keeper) CommitteeSize(ctx sdk.Context, epochID uint64) (int, bool) {
	e, ok := k.GetEpoch(ctx, epochID)
	if !ok {
		return 0, false
	}
	return len(e.Committee), true
}

// GetEpochVerification returns an epoch's published DKG verification material —
// the commitment points (the public polynomial; commits[0] is the epoch master
// key) and the committee size n — for building a reveal-capable threshold
// scheme (Task 1.6 Reveal wiring). ok=false unless the epoch exists and is
// currently decryptable (ACTIVE or DRAINING) with a published key; a FORMING/
// FAILED/RETIRED epoch yields no reveal material.
func (k Keeper) GetEpochVerification(ctx sdk.Context, epochID uint64) (commits [][]byte, n int, ok bool) {
	e, found := k.GetEpoch(ctx, epochID)
	if !found || len(e.Commits) == 0 {
		return nil, 0, false
	}
	if e.Status != types.EPOCH_STATUS_ACTIVE && e.Status != types.EPOCH_STATUS_DRAINING {
		return nil, 0, false
	}
	return e.Commits, len(e.Committee), true
}

// storePrefixEnd returns the exclusive end key for a single-byte prefix scan.
func storePrefixEnd(prefix []byte) []byte {
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
