// CosmWasm wiring helpers (task 2.2, D-077/D-078): the CVM contract runtime
// integrates on the D-077 v0.50 dependency line (wasmd v0.54.x).
package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/spf13/cast"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
)

// wasmVMDir returns the node-local wasmvm directory (compiled-module disk
// cache — never consensus state). On a real node it lives under the
// configured home, like the DKG share store (vote_extensions.go). Tests pass
// no home (sims.EmptyAppOptions); each app instance then gets a FRESH temp
// dir, because wasmvm holds an exclusive file lock on its dir and in-process
// suites (sim, e2e) construct several apps at once.
func wasmVMDir(appOpts servertypes.AppOptions) string {
	if home := cast.ToString(appOpts.Get(flags.FlagHome)); home != "" {
		return filepath.Join(home, "wasm")
	}
	dir, err := os.MkdirTemp("", "conclave-wasm-test")
	if err != nil {
		panic("creating wasm test dir: " + err.Error())
	}
	return dir
}

// permissionedWasmModule wraps the wasm AppModule with ONE delta: the default
// genesis ships the PERMISSIONED code-upload posture (the task-2.2 plan-stop
// decision) instead of wasmd's permissionless default. Code upload and
// default instantiation are Nobody — only governance (wasmd's
// GovAuthorizationPolicy bypasses access config for authority msgs) can store
// code or loosen the params. Arbitrary actors cannot deploy contracts probing
// the CVM binding surface on a settlement chain, even though every binding is
// independently gated (app/cvm).
type permissionedWasmModule struct {
	wasm.AppModule
}

// DefaultGenesis overrides the wasmd default (Everybody/Everybody) with the
// gov-only posture. Params.ValidateBasic accepts it; InitGenesis ships no
// codes or contracts.
func (permissionedWasmModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := wasmtypes.GenesisState{
		Params: wasmtypes.Params{
			CodeUploadAccess:             wasmtypes.AllowNobody,
			InstantiateDefaultPermission: wasmtypes.AccessTypeNobody,
		},
	}
	return cdc.MustMarshalJSON(&gs)
}
