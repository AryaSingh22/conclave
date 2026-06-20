// Package keeper implements x/veil — Sealed Intents and the Reveal
// (SPEC §6.3, Phase 0 STUB mode). The order-then-reveal invariant is real
// even in stub mode: ordering fixes at a session-boundary EndBlock, the
// Reveal runs strictly in the NEXT block's BeginBlock.
package keeper

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	storetypes "cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/crypto/reveal"
	"github.com/AryaSingh22/conclave/x/veil/types"
)

// domainIntent is the D-009 id-derivation domain byte for intents
// (0x01 sigils, 0x02 credentials, 0x03 mandates, 0x04 intents — D-037).
const domainIntent = 0x04

// phase0Chamber is the only active chamber until Phase 3 (SPEC §1.2).
// CONCLAVE-TODO(phase-3): chamber routing once chamber_id > 0 activates.
const phase0Chamber uint32 = 0

// Keeper provides access to the x/veil store (SPEC §6.3).
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService storetypes.KVStoreService

	scheme reveal.ThresholdScheme

	bankKeeper    types.BankKeeper
	distrKeeper   types.DistrKeeper
	sigilKeeper   types.SigilKeeper
	mandateKeeper types.MandateKeeper
	sessionKeeper types.SessionKeeper
	priceSource   types.PriceSource

	// epochKeeper is wired AFTER construction via SetEpochKeeper to break the
	// veil↔epoch cycle (veil needs IsDecryptableEpoch for submit-time epoch-tag
	// validation; epoch needs HasInFlightForEpoch for drain-before-rotate). nil
	// ⇒ no epoch system is wired (stub mode) and the epoch-tag check is skipped.
	epochKeeper types.EpochKeeper

	// authority is the address allowed to update params (gov module account).
	authority string
}

// NewKeeper creates a new x/veil Keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	scheme reveal.ThresholdScheme,
	bankKeeper types.BankKeeper,
	distrKeeper types.DistrKeeper,
	sigilKeeper types.SigilKeeper,
	mandateKeeper types.MandateKeeper,
	sessionKeeper types.SessionKeeper,
	priceSource types.PriceSource,
	authority string,
) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic("invalid x/veil authority address: " + err.Error())
	}
	return Keeper{
		cdc:           cdc,
		storeService:  storeService,
		scheme:        scheme,
		bankKeeper:    bankKeeper,
		distrKeeper:   distrKeeper,
		sigilKeeper:   sigilKeeper,
		mandateKeeper: mandateKeeper,
		sessionKeeper: sessionKeeper,
		priceSource:   priceSource,
		authority:     authority,
	}
}

// SetEpochKeeper wires the x/epoch keeper after both keepers exist (the
// veil↔epoch cycle, D-068). App construction MUST call this before the veil
// AppModule is captured by RegisterModules so the msg-server copy observes it
// (mirrors mandate.SetLiveExposureFn). When unset (veil unit tests), the
// submit-time epoch-tag check is skipped and epoch_id==0 stub intents pass.
func (k *Keeper) SetEpochKeeper(ek types.EpochKeeper) { k.epochKeeper = ek }

// GetAuthority returns the module's authority address.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// GetParams returns the current module parameters.
func (k Keeper) GetParams(ctx sdk.Context) (types.Params, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.ParamsKey)
	if err != nil {
		return types.Params{}, err
	}
	if bz == nil {
		return types.DefaultParams(), nil
	}
	var params types.Params
	if err := k.cdc.Unmarshal(bz, &params); err != nil {
		return types.Params{}, err
	}
	return params, nil
}

// SetParams stores the module parameters.
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	bz, err := k.cdc.Marshal(&params)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(types.ParamsKey, bz)
}

// nextIDSequence increments and returns the global id sequence — the
// "nonce" in §5.1's hash(sender_sigil, nonce, chamber) id derivation
// (D-009 scheme, D-037).
func (k Keeper) nextIDSequence(ctx sdk.Context) (uint64, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.IDSequenceKey)
	if err != nil {
		return 0, err
	}
	var seq uint64
	if bz != nil {
		seq = binary.BigEndian.Uint64(bz)
	}
	seq++
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, seq)
	if err := store.Set(types.IDSequenceKey, out); err != nil {
		return 0, err
	}
	return seq, nil
}

// deriveID derives a deterministic 32-byte id (hex-rendered) per D-009:
// sha256(domain ‖ chamber_be32 ‖ payload ‖ seq_be64).
func deriveID(domain byte, chamberID uint32, payload []byte, seq uint64) string {
	h := sha256.New()
	h.Write([]byte{domain})
	var chamber [4]byte
	binary.BigEndian.PutUint32(chamber[:], chamberID)
	h.Write(chamber[:])
	h.Write(payload)
	var seqBz [8]byte
	binary.BigEndian.PutUint64(seqBz[:], seq)
	h.Write(seqBz[:])
	return hex.EncodeToString(h.Sum(nil))
}

// Phase0Chamber exposes the active chamber for tests and app wiring.
func Phase0Chamber() uint32 { return phase0Chamber }
