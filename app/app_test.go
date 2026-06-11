package app_test

import (
	"encoding/json"
	"testing"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/AryaSingh22/conclave/app"
)

// TestBech32Prefixes asserts the chain identity from SPEC §0: accounts are
// conc1..., validators concvaloper1....
func TestBech32Prefixes(t *testing.T) {
	cfg := sdk.GetConfig()
	if got := cfg.GetBech32AccountAddrPrefix(); got != "conc" {
		t.Fatalf("account bech32 prefix: got %q, want %q", got, "conc")
	}
	if got := cfg.GetBech32ValidatorAddrPrefix(); got != "concvaloper" {
		t.Fatalf("validator bech32 prefix: got %q, want %q", got, "concvaloper")
	}
	if got := cfg.GetBech32ConsensusAddrPrefix(); got != "concvalcons" {
		t.Fatalf("consensus bech32 prefix: got %q, want %q", got, "concvalcons")
	}

	addr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	if got := addr.String(); len(got) < 5 || got[:5] != "conc1" {
		t.Fatalf("rendered account address %q does not start with conc1", got)
	}
}

// bootConclaveApp constructs the app on the given backend, runs InitChain
// with a single bonded validator, and finalizes+commits the requested number
// of blocks.
func bootConclaveApp(t *testing.T, db dbm.DB, blocks int) *app.ConclaveApp {
	t.Helper()

	conclaveApp := app.NewConclaveApp(
		log.NewNopLogger(), db, nil, true, sims.EmptyAppOptions{},
		baseapp.SetChainID("conclave-dev-1"),
	)

	genesisState := conclaveApp.DefaultGenesis()
	for _, name := range []string{"capability", "ibc", "transfer"} {
		if _, ok := genesisState[name]; !ok {
			t.Fatalf("default genesis is missing manually wired module %q", name)
		}
	}

	valSet, err := sims.CreateRandomValidatorSet()
	if err != nil {
		t.Fatalf("creating validator set: %v", err)
	}

	senderPrivKey := secp256k1.GenPrivKey()
	acc := authtypes.NewBaseAccount(senderPrivKey.PubKey().Address().Bytes(), senderPrivKey.PubKey(), 0, 0)
	balance := banktypes.Balance{
		Address: acc.GetAddress().String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(app.BaseDenom, math.NewInt(10_000_000_000))),
	}

	genesisState, err = sims.GenesisStateWithValSet(
		conclaveApp.AppCodec(), genesisState, valSet,
		[]authtypes.GenesisAccount{acc}, balance,
	)
	if err != nil {
		t.Fatalf("building genesis state: %v", err)
	}

	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	if err != nil {
		t.Fatalf("marshaling genesis state: %v", err)
	}

	if _, err := conclaveApp.InitChain(&abci.RequestInitChain{
		ChainId:         "conclave-dev-1",
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: sims.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	}); err != nil {
		t.Fatalf("InitChain: %v", err)
	}

	for i := 0; i < blocks; i++ {
		if _, err := conclaveApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: conclaveApp.LastBlockHeight() + 1,
			Hash:   conclaveApp.LastCommitID().Hash,
		}); err != nil {
			t.Fatalf("FinalizeBlock at height %d: %v", conclaveApp.LastBlockHeight()+1, err)
		}
		if _, err := conclaveApp.Commit(); err != nil {
			t.Fatalf("Commit at height %d: %v", conclaveApp.LastBlockHeight(), err)
		}
	}

	return conclaveApp
}

// TestConclaveAppBoots is the minimal proof that the hybrid depinject +
// manual-IBC wiring produces a working state machine (SPEC §10 task 0.1).
func TestConclaveAppBoots(t *testing.T) {
	conclaveApp := bootConclaveApp(t, dbm.NewMemDB(), 1)

	if got := conclaveApp.LastBlockHeight(); got != 1 {
		t.Fatalf("LastBlockHeight after first commit: got %d, want 1", got)
	}

	// Historical-state queries must work: this is the exact path used by
	// gRPC/CLI queries (CacheMultiStoreWithVersion over every mounted store,
	// including the manually registered IBC stores).
	if _, err := conclaveApp.CreateQueryContext(1, false); err != nil {
		t.Fatalf("CreateQueryContext(1): %v", err)
	}
}

// TestQueryContextOnDiskBackend boots the app on goleveldb — the backend the
// real node uses — and asserts historical queries work.
//
// Regression guard for the go.mod goleveldb replace directive: with the
// broken upstream goleveldb release, IAVL version lookups fail for stores
// that receive no writes at genesis (authz, evidence, feegrant, params),
// which breaks EVERY query on a running node with "version does not exist".
// replace directives do not propagate from cosmos-sdk; this repo must carry
// its own.
func TestQueryContextOnDiskBackend(t *testing.T) {
	db, err := dbm.NewGoLevelDB("application", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("opening goleveldb: %v", err)
	}
	defer db.Close()

	conclaveApp := bootConclaveApp(t, db, 3)

	for h := int64(1); h <= conclaveApp.LastBlockHeight(); h++ {
		if _, err := conclaveApp.CreateQueryContext(h, false); err != nil {
			t.Fatalf("CreateQueryContext(%d) on goleveldb: %v", h, err)
		}
	}
}
