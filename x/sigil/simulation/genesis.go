// Package simulation implements the SDK module simulation hooks for x/sigil
// (task 0.11, SPEC §10 row 0.11): randomized genesis, weighted operations that
// generate random valid identity msgs, and the store decoder.
package simulation

import (
	"crypto/sha256"
	"encoding/hex"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

// RandomizedGenState pre-registers Sigils for a random subset of the simulation
// accounts so the veil/mandate/session operations have identity material to act
// on from block 1 (the RegisterSigil operation grows the set further during the
// run). Params are held at their dev defaults — task 0.11 fuzzes cross-module
// operation INTERACTIONS, not params (the param boundaries are already covered
// by the per-module fuzz suites), so the run stays focused and its determinism
// reasoning stays clean.
func RandomizedGenState(simState *module.SimulationState) {
	gs := types.DefaultGenesis()

	// pre-register ~40% of accounts as sigil controllers.
	n := len(simState.Accounts)
	preReg := n * 2 / 5
	for i := 0; i < preReg; i++ {
		acc := simState.Accounts[i]
		id := deriveGenesisSigilID(acc.Address.Bytes())
		gs.Sigils = append(gs.Sigils, types.Sigil{
			Id:                id,
			ControllerAddr:    acc.Address.String(),
			DidDocHash:        genesisDIDHash(acc.Address.Bytes()),
			MetadataUri:       "sim://sigil/" + id[:8],
			RecoverySet:       nil,
			RecoveryThreshold: 0,
			CreatedHeight:     0,
			ChamberId:         0,
		})
		gs.Reputations = append(gs.Reputations, types.Reputation{
			SigilId:         id,
			Score:           math.LegacyZeroDec(),
			LastDecayHeight: 0,
			ChamberId:       0,
		})
	}

	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(gs)
}

// deriveGenesisSigilID produces a deterministic 64-hex (32-byte) id from a
// controller address — distinct from the keeper's sequence-salted derivation
// (these are genesis fixtures, not msg-server output) but shape-compatible with
// SigilIDBytes.
func deriveGenesisSigilID(controller []byte) string {
	h := sha256.Sum256(append([]byte{0x01, 'g'}, controller...))
	return hex.EncodeToString(h[:])
}

func genesisDIDHash(controller []byte) []byte {
	h := sha256.Sum256(append([]byte{0xDD}, controller...))
	return h[:]
}
