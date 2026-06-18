// Package keeper implements the x/mandate agent policy module — the
// Phase-0 centerpiece (SPEC §6.2).
package keeper

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	storetypes "cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// domainMandate is the D-009 id-derivation domain byte for mandates
// (0x01 = sigils, 0x02 = credentials, 0x03 = mandates — D-016).
const domainMandate = 0x03

// Keeper provides access to the x/mandate store (SPEC §6.2).
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService storetypes.KVStoreService

	sigilKeeper   types.SigilKeeper
	bankKeeper    types.BankKeeper
	accountKeeper types.AccountKeeper

	// authority is the address allowed to update params (gov module account).
	authority string

	// liveExposureFn, when wired at app construction, returns the grantee's
	// LIVE intent exposure (Σ charged_spend over non-terminated intents).
	// IssueMandate folds it into a FRESH mandate's outstanding_commitment so a
	// key transitioning root->mandated is bounded from the instant of issuance
	// (D-064; D-059 Option X extended to the issuance boundary). It is injected
	// via SetLiveExposureFn rather than imported, so the mandate keeper stays
	// free of a compile-time x/veil edge (D-058 keeps cross-module accounting at
	// app level) and the veil↔mandate cycle is broken the same way SetVeilKeeper
	// breaks veil↔session. nil => no adoption (mandate unit tests without veil).
	liveExposureFn func(ctx sdk.Context, chamberID uint32, grantee sdk.AccAddress) (sdk.Coins, error)
}

// SetLiveExposureFn wires the x/veil live-exposure scan used by IssueMandate to
// adopt a root->mandated key's in-flight commitments at issuance (D-064). The
// app calls this once both keepers exist, BEFORE the keeper value is captured by
// the module/ante wiring, so every copy observes it (mirrors SetVeilKeeper).
func (k *Keeper) SetLiveExposureFn(fn func(ctx sdk.Context, chamberID uint32, grantee sdk.AccAddress) (sdk.Coins, error)) {
	k.liveExposureFn = fn
}

// NewKeeper creates a new x/mandate Keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	sigilKeeper types.SigilKeeper,
	bankKeeper types.BankKeeper,
	accountKeeper types.AccountKeeper,
	authority string,
) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic("invalid x/mandate authority address: " + err.Error())
	}
	return Keeper{
		cdc:           cdc,
		storeService:  storeService,
		sigilKeeper:   sigilKeeper,
		bankKeeper:    bankKeeper,
		accountKeeper: accountKeeper,
		authority:     authority,
	}
}

// GetAuthority returns the module's authority address.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// Codec returns the module codec; the MandateDecorator uses it to unpack
// nested msgs during the SPEC §6.2 step 3 recursive TypeURL walk.
func (k Keeper) Codec() codec.BinaryCodec {
	return k.cdc
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

// nextIDSequence increments and returns the global id sequence used to salt
// mandate id derivation (D-009 scheme).
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
