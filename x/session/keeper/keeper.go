// Package keeper implements x/session — batch clearing and CONCLUDE
// (SPEC §6.4). The session keeper replaces the D-038 StubSessionKeeper:
// SessionBoundary/IntakeRevealed feed x/veil's Reveal hand-off, and the
// keeper owns the solver registry, solution validation, the atomic Conclude,
// scoring/fallback (Half B), and the §6.4 EndBlock sweeps.
package keeper

import (
	"crypto/sha256"
	"encoding/binary"

	storetypes "cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// phase0Chamber is the only active chamber until Phase 3 (SPEC §1.2).
// CONCLAVE-TODO(phase-3): chamber routing once chamber_id > 0 activates.
const phase0Chamber uint32 = 0

// domainEscrow / domainReceipt / domainSolution are the id-derivation domain
// bytes for escrow-stub, §6.6 receipt, and fallback-solution ids
// (session-internal namespace; sha256 like D-009).
const (
	domainEscrow  = 0x10
	domainReceipt = 0x11
	// domainSolution derives the id of the SYNTHETIC Solution record the
	// fallback path persists (D-086). Solver-proposed solution ids are
	// tx-hash-derived (§6.4), so the domain-separated derivation cannot
	// collide with them short of a sha256 collision.
	domainSolution = 0x12
)

// Keeper provides access to the x/session store (SPEC §6.4).
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService storetypes.KVStoreService

	bankKeeper    types.BankKeeper
	distrKeeper   types.DistrKeeper
	accountKeeper types.AccountKeeper
	sigilKeeper   types.SigilKeeper
	// escrowKeeper owns the SPEC §6.5 conditioned-delivery escrows (D-073
	// ownership handoff). CONCLUDE writes escrow records through it instead of
	// into the session store; the call runs inside the atomic Conclude cache.
	escrowKeeper types.EscrowKeeper

	// veilKeeper is wired AFTER construction via SetVeilKeeper to break the
	// veil↔session dependency cycle (veil needs SessionBoundary/IntakeRevealed;
	// session needs the intent store + refund matrix). Guarded by veil().
	veilKeeper types.VeilKeeper

	// authority is the address allowed to update params (gov module account).
	authority string
}

// NewKeeper creates a new x/session Keeper. The keeper is returned as a
// pointer so SetVeilKeeper can wire the cyclic veil dependency in place.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	bankKeeper types.BankKeeper,
	distrKeeper types.DistrKeeper,
	accountKeeper types.AccountKeeper,
	sigilKeeper types.SigilKeeper,
	escrowKeeper types.EscrowKeeper,
	authority string,
) *Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic("invalid x/session authority address: " + err.Error())
	}
	return &Keeper{
		cdc:           cdc,
		storeService:  storeService,
		bankKeeper:    bankKeeper,
		distrKeeper:   distrKeeper,
		accountKeeper: accountKeeper,
		sigilKeeper:   sigilKeeper,
		escrowKeeper:  escrowKeeper,
		authority:     authority,
	}
}

// SetVeilKeeper wires the x/veil keeper after both keepers exist (Guard 1).
// App construction MUST call this before any block runs; veil() panics with a
// clear message if it does not.
func (k *Keeper) SetVeilKeeper(vk types.VeilKeeper) {
	k.veilKeeper = vk
}

// veil returns the wired VeilKeeper, panicking with a clear message if the
// cyclic dependency was never wired (Guard 1: SetVeilKeeper must run during
// app construction, before any block touches settlement).
func (k Keeper) veil() types.VeilKeeper {
	if k.veilKeeper == nil {
		panic("x/session: VeilKeeper is nil — app wiring must call SetVeilKeeper before any block")
	}
	return k.veilKeeper
}

// GetAuthority returns the module's authority address.
func (k Keeper) GetAuthority() string { return k.authority }

// ModuleAddress returns the session module account — the SPEC §6.4 settlement
// escrow account, solver-bond custodian, fee pool, and svc/{schema} minter.
func (k Keeper) ModuleAddress() sdk.AccAddress {
	return k.accountKeeper.GetModuleAddress(types.ModuleName)
}

// GetParams returns the current module parameters.
func (k Keeper) GetParams(ctx sdk.Context) (types.Params, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.ParamsKey)
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

// u64be renders a uint64 as 8 big-endian bytes (id-derivation payloads).
func u64be(v uint64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, v)
	return bz
}

// deriveID derives a deterministic 32-byte id (hex-rendered) per the D-009
// scheme: sha256(domain ‖ chamber_be32 ‖ payload).
func deriveID(domain byte, chamberID uint32, payload []byte) string {
	h := sha256.New()
	h.Write([]byte{domain})
	var chamber [4]byte
	binary.BigEndian.PutUint32(chamber[:], chamberID)
	h.Write(chamber[:])
	h.Write(payload)
	return types.IDHex(h.Sum(nil))
}
