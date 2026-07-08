package app_test

// Task 2.2 (D-078) load-bearing CVM binding suite.
//
// Two complementary drivers, because the reflect test contract (wasmd's
// standard custom-msg vehicle) types its Custom msg/query as its own enum
// (debug|raw / ping|capitalized) and cannot spell Conclave's schema:
//
//   - REAL-CONTRACT subtests run reflect through wasmvm end to end and prove
//     the wasm→binding wiring: a contract-emitted custom msg lands in the CVM
//     allowlist (and rejects), a contract-emitted Any(MsgConclude) is
//     rejected by the layer-2 passthrough gate, a contract-issued chain query
//     lands in the CVM querier, stargate queries stay rejected, and the
//     permissioned-upload posture holds.
//   - DISPATCH subtests replicate wasmd's SDKMessageHandler steps exactly —
//     encode via the production cvm encoder with the contract's address,
//     verify every signer IS the contract (wasmd's dispatch check), route via
//     the real MsgServiceRouter — and prove the keeper-side authority model:
//     a contract releases a HASHLOCK escrow only with the preimage
//     (knowledge gate), cannot dispute an escrow whose payer it is not
//     (identity gate), and MsgConclude under a contract authority is
//     rejected by the session keeper (layer 3).

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/CosmWasm/wasmd/x/wasm/keeper/testdata"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/v2/types"

	"github.com/AryaSingh22/conclave/app"
	"github.com/AryaSingh22/conclave/app/cvm"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	sessionkeeper "github.com/AryaSingh22/conclave/x/session/keeper"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
)

// cvmFixture boots the app with two funded accounts (buyer/payer and
// seller/payee) and returns a context above the committed height for
// keeper-level driving.
type cvmFixture struct {
	app    *app.ConclaveApp
	ctx    sdk.Context
	buyer  sdk.AccAddress
	seller sdk.AccAddress
}

func bootCVMFixture(t *testing.T) *cvmFixture {
	t.Helper()

	conclaveApp := app.NewConclaveApp(
		log.NewNopLogger(), dbm.NewMemDB(), nil, true, sims.EmptyAppOptions{},
		baseapp.SetChainID("conclave-dev-1"),
	)

	buyerKey := secp256k1.GenPrivKey()
	sellerKey := secp256k1.GenPrivKey()
	buyerAcc := authtypes.NewBaseAccount(buyerKey.PubKey().Address().Bytes(), buyerKey.PubKey(), 0, 0)
	sellerAcc := authtypes.NewBaseAccount(sellerKey.PubKey().Address().Bytes(), sellerKey.PubKey(), 1, 0)
	fund := sdk.NewCoins(sdk.NewCoin(app.BaseDenom, math.NewInt(10_000_000_000)))

	valSet, err := sims.CreateRandomValidatorSet()
	if err != nil {
		t.Fatalf("creating validator set: %v", err)
	}
	genesisState, err := sims.GenesisStateWithValSet(
		conclaveApp.AppCodec(), conclaveApp.DefaultGenesis(), valSet,
		[]authtypes.GenesisAccount{buyerAcc, sellerAcc},
		banktypes.Balance{Address: buyerAcc.GetAddress().String(), Coins: fund},
		banktypes.Balance{Address: sellerAcc.GetAddress().String(), Coins: fund},
	)
	if err != nil {
		t.Fatalf("building genesis state: %v", err)
	}
	stateBytes, err := json.Marshal(genesisState)
	if err != nil {
		t.Fatalf("marshaling genesis state: %v", err)
	}
	if _, err := conclaveApp.InitChain(&abci.RequestInitChain{
		ChainId:         "conclave-dev-1",
		ConsensusParams: sims.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	}); err != nil {
		t.Fatalf("InitChain: %v", err)
	}
	if _, err := conclaveApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: 1, Hash: conclaveApp.LastCommitID().Hash,
	}); err != nil {
		t.Fatalf("FinalizeBlock: %v", err)
	}
	if _, err := conclaveApp.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	ctx := conclaveApp.NewUncachedContext(false, cmtproto.Header{
		Height:  conclaveApp.LastBlockHeight() + 1,
		ChainID: "conclave-dev-1",
		Time:    time.Unix(1_750_000_000, 0).UTC(),
	})

	return &cvmFixture{
		app:    conclaveApp,
		ctx:    ctx,
		buyer:  buyerAcc.GetAddress(),
		seller: sellerAcc.GetAddress(),
	}
}

// storeReflect stores + instantiates the reflect contract through the gov
// permission path (the only path the permissioned posture allows).
func (f *cvmFixture) storeReflect(t *testing.T) sdk.AccAddress {
	t.Helper()
	gov := wasmkeeper.NewGovPermissionKeeper(&f.app.WasmKeeper)
	codeID, _, err := gov.Create(f.ctx, f.buyer, testdata.ReflectContractWasm(), nil)
	if err != nil {
		t.Fatalf("gov store code: %v", err)
	}
	contractAddr, _, err := gov.Instantiate(f.ctx, codeID, f.buyer, nil, []byte("{}"), "cvm reflect", nil)
	if err != nil {
		t.Fatalf("gov instantiate: %v", err)
	}
	return contractAddr
}

// execReflect has the real reflect contract emit one CosmosMsg through wasmvm.
func (f *cvmFixture) execReflect(t *testing.T, contractAddr sdk.AccAddress, msg wasmvmtypes.CosmosMsg) error {
	t.Helper()
	handle := testdata.ReflectHandleMsg{
		Reflect: &testdata.ReflectPayload{Msgs: []wasmvmtypes.CosmosMsg{msg}},
	}
	bz, err := json.Marshal(handle)
	if err != nil {
		t.Fatalf("marshaling reflect msg: %v", err)
	}
	perm := wasmkeeper.NewDefaultPermissionKeeper(&f.app.WasmKeeper)
	_, err = perm.Execute(f.ctx, contractAddr, f.buyer, bz, nil)
	return err
}

// dispatchCVM replicates wasmd's SDKMessageHandler for a custom msg, step for
// step: encode with the PRODUCTION cvm encoder under the contract's address,
// enforce wasmd's dispatch invariant that every signer of the encoded msg IS
// the contract, then route through the real MsgServiceRouter.
func (f *cvmFixture) dispatchCVM(t *testing.T, ctx sdk.Context, contractAddr sdk.AccAddress, custom string) error {
	t.Helper()
	msgs, err := cvm.EncodeCustomMsg(contractAddr, json.RawMessage(custom))
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		signers, _, err := f.app.AppCodec().GetMsgV1Signers(msg)
		if err != nil {
			t.Fatalf("resolving signers: %v", err)
		}
		for _, s := range signers {
			if !sdk.AccAddress(s).Equals(contractAddr) {
				t.Fatalf("encoder produced signer %s that is not the contract %s — the stamping invariant is broken",
					sdk.AccAddress(s), contractAddr)
			}
		}
		handler := f.app.MsgServiceRouter().Handler(msg)
		if handler == nil {
			t.Fatalf("no msg-service handler for %T", msg)
		}
		if _, err := handler(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

// escrowID renders a deterministic 32-byte hex id.
func escrowID(seed byte) string {
	b := make([]byte, escrowtypes.IDLength)
	for i := range b {
		b[i] = seed
	}
	return hex.EncodeToString(b)
}

func TestCVMBindings(t *testing.T) {
	f := bootCVMFixture(t)

	// Sigils for the escrow parties: payer = buyer, payee = seller.
	didHash := make([]byte, 32)
	payerSigil, err := f.app.SigilKeeper.RegisterSigil(f.ctx, f.buyer, didHash, "", nil, 0, 0)
	if err != nil {
		t.Fatalf("registering payer sigil: %v", err)
	}
	payeeSigil, err := f.app.SigilKeeper.RegisterSigil(f.ctx, f.seller, didHash, "", nil, 0, 0)
	if err != nil {
		t.Fatalf("registering payee sigil: %v", err)
	}

	t.Run("permissioned upload", func(t *testing.T) {
		params := f.app.WasmKeeper.GetParams(f.ctx)
		if params.CodeUploadAccess.Permission != wasmtypes.AccessTypeNobody {
			t.Fatalf("code upload access is %s, want Nobody (the task-2.2 posture)", params.CodeUploadAccess.Permission)
		}
		if params.InstantiateDefaultPermission != wasmtypes.AccessTypeNobody {
			t.Fatalf("instantiate default is %s, want Nobody", params.InstantiateDefaultPermission)
		}
		perm := wasmkeeper.NewDefaultPermissionKeeper(&f.app.WasmKeeper)
		if _, _, err := perm.Create(f.ctx, f.buyer, testdata.ReflectContractWasm(), nil); err == nil {
			t.Fatal("non-gov code upload must be rejected under the permissioned posture")
		}
	})

	contractAddr := f.storeReflect(t)

	t.Run("real contract: custom msg lands in the CVM allowlist and rejects", func(t *testing.T) {
		// reflect's typed Custom can spell {"debug": ...} — not an allowlisted
		// CVM msg. The full wasm→handler→cvm.EncodeCustomMsg path must reject
		// it WITH the allowlist's provenance, proving the production encoder
		// is the one wired in.
		err := f.execReflect(t, contractAddr, wasmvmtypes.CosmosMsg{
			Custom: json.RawMessage(`{"debug":"forge probe"}`),
		})
		if err == nil {
			t.Fatal("a non-allowlisted custom msg from a real contract must be rejected")
		}
		if !strings.Contains(err.Error(), "CVM custom msg (allowlist)") {
			t.Fatalf("rejection must come from the CVM allowlist, got: %v", err)
		}
	})

	t.Run("real contract: any passthrough to MsgConclude rejected", func(t *testing.T) {
		conclude := &sessiontypes.MsgConclude{
			Authority: authtypes.NewModuleAddress("gov").String(),
			SessionId: 1,
		}
		bz, err := conclude.Marshal()
		if err != nil {
			t.Fatalf("marshaling MsgConclude: %v", err)
		}
		err = f.execReflect(t, contractAddr, wasmvmtypes.CosmosMsg{
			Any: &wasmvmtypes.AnyMsg{TypeURL: sdk.MsgTypeURL(conclude), Value: bz},
		})
		if err == nil {
			t.Fatal("Any(MsgConclude) from a real contract must be rejected")
		}
		if !strings.Contains(err.Error(), "passthrough is disabled") {
			t.Fatalf("rejection must come from the CVM layer-2 gate, got: %v", err)
		}
	})

	t.Run("forge: contract authority rejected by session keeper", func(t *testing.T) {
		msgServer := sessionkeeper.NewMsgServerImpl(f.app.SessionKeeper)
		_, err := msgServer.Conclude(f.ctx, &sessiontypes.MsgConclude{
			Authority: contractAddr.String(),
			SessionId: 1,
		})
		if err == nil {
			t.Fatal("MsgConclude with a contract authority must be rejected (layer 3)")
		}
		if !errors.Is(err, sessiontypes.ErrUnauthorized) {
			t.Fatalf("want session ErrUnauthorized, got: %v", err)
		}
	})

	preimage := []byte("cvm-e2e-preimage")
	payloadHash := sha256.Sum256(preimage)
	amount := sdk.NewCoins(sdk.NewInt64Coin(app.BaseDenom, 5_000))

	hashlockID := escrowID(0xA1)
	hashlockEscrow := escrowtypes.Escrow{
		Id:         hashlockID,
		PayerSigil: payerSigil,
		PayeeSigil: payeeSigil,
		Amount:     amount,
		Condition: escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Hashlock{
			Hashlock: &escrowtypes.HashlockCondition{PayloadHash: payloadHash[:], TimeoutBlocks: 1_000},
		}},
		ChamberId: 0,
	}
	if err := f.app.EscrowKeeper.CreateEscrow(f.ctx, hashlockEscrow, f.buyer, f.seller, nil); err != nil {
		t.Fatalf("creating HASHLOCK escrow: %v", err)
	}

	t.Run("dispatch: wrong preimage from contract rejected at the knowledge gate", func(t *testing.T) {
		bad := base64.StdEncoding.EncodeToString([]byte("wrong-preimage"))
		err := f.dispatchCVM(t, f.ctx, contractAddr,
			fmt.Sprintf(`{"release_escrow":{"escrow_id":%q,"preimage":%q}}`, hashlockID, bad))
		if !errors.Is(err, escrowtypes.ErrPreimageMismatch) {
			t.Fatalf("want ErrPreimageMismatch, got: %v", err)
		}
	})

	t.Run("dispatch: contract releases hashlock with preimage, gas metered", func(t *testing.T) {
		sellerBefore := f.app.BankKeeper.GetBalance(f.ctx, f.seller, app.BaseDenom)

		gasCtx := f.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
		good := base64.StdEncoding.EncodeToString(preimage)
		err := f.dispatchCVM(t, gasCtx, contractAddr,
			fmt.Sprintf(`{"release_escrow":{"escrow_id":%q,"preimage":%q}}`, hashlockID, good))
		if err != nil {
			t.Fatalf("contract release with valid preimage: %v", err)
		}
		if consumed := gasCtx.GasMeter().GasConsumed(); consumed == 0 {
			t.Fatal("a CVM-dispatched native release must consume gas on the caller's meter")
		}

		esc, err := f.app.EscrowKeeper.GetEscrow(f.ctx, 0, hashlockID)
		if err != nil {
			t.Fatalf("reading escrow: %v", err)
		}
		if esc.State != escrowtypes.ESCROW_STATE_RELEASED {
			t.Fatalf("escrow state %s, want RELEASED", esc.State)
		}
		sellerAfter := f.app.BankKeeper.GetBalance(f.ctx, f.seller, app.BaseDenom)
		if !sellerAfter.Sub(sellerBefore).Amount.Equal(math.NewInt(5_000)) {
			t.Fatalf("seller balance delta %s, want 5000", sellerAfter.Sub(sellerBefore))
		}
	})

	optimisticID := escrowID(0xB2)
	optimisticEscrow := escrowtypes.Escrow{
		Id:         optimisticID,
		PayerSigil: payerSigil,
		PayeeSigil: payeeSigil,
		Amount:     amount,
		Condition: escrowtypes.DeliveryCondition{Condition: &escrowtypes.DeliveryCondition_Optimistic{
			Optimistic: &escrowtypes.OptimisticCondition{DisputeWindowBlocks: 1_000},
		}},
		ChamberId: 0,
	}
	if err := f.app.EscrowKeeper.CreateEscrow(f.ctx, optimisticEscrow, f.buyer, f.seller, sha256Bytes([]byte("content"))); err != nil {
		t.Fatalf("creating OPTIMISTIC escrow: %v", err)
	}

	t.Run("dispatch: contract that is not the payer cannot dispute", func(t *testing.T) {
		err := f.dispatchCVM(t, f.ctx, contractAddr,
			fmt.Sprintf(`{"raise_dispute":{"escrow_id":%q}}`, optimisticID))
		if !errors.Is(err, escrowtypes.ErrUnauthorized) {
			t.Fatalf("want escrow ErrUnauthorized (identity gate), got: %v", err)
		}
	})

	t.Run("querier: bounded escrow read returns the record", func(t *testing.T) {
		plugins := cvm.QueryPlugins(f.app.AppCodec(), f.app.SessionKeeper, f.app.EscrowKeeper)
		data, err := plugins.Custom(f.ctx, json.RawMessage(
			fmt.Sprintf(`{"escrow":{"chamber_id":0,"escrow_id":%q}}`, optimisticID)))
		if err != nil {
			t.Fatalf("escrow query: %v", err)
		}
		if !strings.Contains(string(data), optimisticID) {
			t.Fatalf("escrow query response does not contain the escrow id: %s", data)
		}
	})

	t.Run("querier: unknown query shape rejected", func(t *testing.T) {
		plugins := cvm.QueryPlugins(f.app.AppCodec(), f.app.SessionKeeper, f.app.EscrowKeeper)
		if _, err := plugins.Custom(f.ctx, json.RawMessage(`{"ping":{}}`)); err == nil {
			t.Fatal("a non-allowlisted custom query must be rejected")
		}
		if _, err := plugins.Custom(f.ctx, json.RawMessage(`{"receipt":{"receipt_id":"nope"}}`)); err == nil {
			t.Fatal("a missing receipt must be an error, not empty data")
		}
	})

	t.Run("real contract: chain query lands in the CVM querier", func(t *testing.T) {
		// reflect's typed CustomQuery can spell {"ping":{}} — not a CVM query.
		// The full wasm→querier→cvm path must reject it with the CVM
		// querier's provenance, proving the production querier is wired in.
		query := testdata.ReflectQueryMsg{
			Chain: &testdata.ChainQuery{
				Request: &wasmvmtypes.QueryRequest{Custom: json.RawMessage(`{"ping":{}}`)},
			},
		}
		bz, err := json.Marshal(query)
		if err != nil {
			t.Fatalf("marshaling reflect query: %v", err)
		}
		_, err = f.app.WasmKeeper.QuerySmart(f.ctx, contractAddr, bz)
		if err == nil {
			t.Fatal("a non-allowlisted custom query from a real contract must be rejected")
		}
		if !strings.Contains(err.Error(), "CVM custom query") {
			t.Fatalf("rejection must come from the CVM querier, got: %v", err)
		}
	})

	t.Run("real contract: stargate query stays rejected", func(t *testing.T) {
		query := testdata.ReflectQueryMsg{
			Chain: &testdata.ChainQuery{
				Request: &wasmvmtypes.QueryRequest{Stargate: &wasmvmtypes.StargateQuery{
					Path: "/conclave.session.v1.Query/Params",
					Data: []byte{},
				}},
			},
		}
		bz, err := json.Marshal(query)
		if err != nil {
			t.Fatalf("marshaling reflect query: %v", err)
		}
		if _, err := f.app.WasmKeeper.QuerySmart(f.ctx, contractAddr, bz); err == nil {
			t.Fatal("stargate queries must stay rejected (wasmd default kept)")
		}
	})
}

func sha256Bytes(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}
