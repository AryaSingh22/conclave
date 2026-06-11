// Package keeper implements the x/sigil agent identity module (SPEC §6.1).
package keeper

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	storetypes "cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

// Keeper provides access to the x/sigil store (SPEC §6.1).
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService storetypes.KVStoreService

	// authority is the address allowed to update params (gov module account).
	authority string
}

// NewKeeper creates a new x/sigil Keeper.
func NewKeeper(cdc codec.BinaryCodec, storeService storetypes.KVStoreService, authority string) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic("invalid x/sigil authority address: " + err.Error())
	}
	return Keeper{
		cdc:          cdc,
		storeService: storeService,
		authority:    authority,
	}
}

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

// nextIDSequence increments and returns the global id sequence used to salt
// sigil/credential id derivation.
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

// deriveID derives a deterministic 32-byte id (hex-rendered) from a domain
// prefix, chamber, payload, and the global sequence.
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
