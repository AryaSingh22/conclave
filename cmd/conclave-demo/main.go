// Command conclave-demo is the chain-interaction driver behind
// scripts/demo_e2e.sh — the SPEC §1.3 acceptance scenario. It is a gRPC client
// (reusing the cmd/unordered-blast signing/broadcast pattern) that performs the
// non-solver steps and assertions; scripts/demo_e2e.sh owns process lifecycle
// (booting the node, starting/killing conclave-solver). It is split into
// subcommands so the script can run a phase, (re)start the solver, then run the
// next phase as a fresh process that re-reads on-chain state:
//
//	setup            §1.3 steps 1-3: sigils, alice's mandate, over-ceiling reject
//	cycle-and-assert §1.3 steps 4-6: submit the 3-party cycle, wait for the
//	                 running solver to clear it, assert receipts + reputation
//	fallback         §1.3 step 7: with the solver dead, a 2-party swap clears via
//	                 the in-consensus fallback, while a fresh 3-party cycle does
//	                 NOT clear (proving solver/cycles is load-bearing)
//
// Dev tooling only — not consensus code.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/AryaSingh22/conclave/app"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

const (
	datasetTag   = "conclave-demo-dataset-D"
	svcSchema    = "cmpt"
	aliceSession = "alice-session"
	uusdc        = "uusdc"
	uclv         = "uclv"
	oneCLV       = 1_000_000
	usdc         = 1_000_000 // 1 USDC = 1e6 uusdc
	settleTTL    = 1_000     // blocks ahead; far enough not to expire mid-demo
	waitTimeout  = 90 * time.Second
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: conclave-demo <setup|cycle-and-assert|fallback|x402-check> [flags]")
		os.Exit(2)
	}
	sub := os.Args[1]
	grpcAddr := os.Getenv("CONCLAVE_GRPC")
	if grpcAddr == "" {
		grpcAddr = "localhost:9090"
	}
	home := os.Getenv("CONCLAVED_HOME")
	if home == "" {
		home = os.ExpandEnv("$HOME/.conclaved")
	}
	chainID := os.Getenv("CONCLAVE_CHAIN_ID")
	if chainID == "" {
		chainID = "conclave-dev-1"
	}

	c, err := newCLI(grpcAddr, home, chainID)
	if err != nil {
		fail(err)
	}
	defer c.conn.Close()

	switch sub {
	case "setup":
		err = c.setup()
	case "cycle-and-assert":
		err = c.cycleAndAssert()
	case "fallback":
		err = c.fallback()
	case "x402-check":
		err = c.x402Check()
	case "veil-e2e":
		err = c.veilE2E()
	case "veil-f4-adversary":
		err = c.veilF4Adversary()
	default:
		err = fmt.Errorf("unknown subcommand %q", sub)
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "DEMO FAIL:", err)
	os.Exit(1)
}

type cli struct {
	chainID  string
	conclave *app.ConclaveApp
	kr       keyring.Keyring
	conn     *grpc.ClientConn
	txc      txtypes.ServiceClient
	authc    authtypes.QueryClient
	cmtc     cmtservice.ServiceClient
	sigilc   sigiltypes.QueryClient
	veilc    veiltypes.QueryClient
	sessionc sessiontypes.QueryClient
	receiptc receipttypes.QueryClient
}

func newCLI(grpcAddr, home, chainID string) (*cli, error) {
	conclave := app.NewConclaveApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true, sims.EmptyAppOptions{})
	// force the SDK's gogo-aware codec on the connection: grpc-go's default
	// codec decodes through protobuf-go reflection and panics on gogoproto
	// customtype fields ("invalid Go type math.LegacyDec ...") — first hit
	// by x402-check's Twap read (task 2.3, D-083); sigil Reputation has the
	// same shape
	conn, err := grpc.NewClient(grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(codec.NewProtoCodec(conclave.InterfaceRegistry()).GRPCCodec())),
	)
	if err != nil {
		return nil, err
	}
	kr, err := keyring.New("conclave", keyring.BackendTest, home, nil, conclave.AppCodec())
	if err != nil {
		return nil, err
	}
	return &cli{
		chainID:  chainID,
		conclave: conclave,
		kr:       kr,
		conn:     conn,
		txc:      txtypes.NewServiceClient(conn),
		authc:    authtypes.NewQueryClient(conn),
		cmtc:     cmtservice.NewServiceClient(conn),
		sigilc:   sigiltypes.NewQueryClient(conn),
		veilc:    veiltypes.NewQueryClient(conn),
		sessionc: sessiontypes.NewQueryClient(conn),
		receiptc: receipttypes.NewQueryClient(conn),
	}, nil
}

// ---- §1.3 steps 1-3 ---------------------------------------------------------

func (c *cli) setup() error {
	alice, err := c.key("alice")
	if err != nil {
		return err
	}
	bob, err := c.key("bob")
	if err != nil {
		return err
	}
	carol, err := c.key("carol")
	if err != nil {
		return err
	}

	// step 1: a Sigil for each agent (controller = the agent's own key).
	aliceSigil, err := c.ensureSigil("agent-alice", alice)
	if err != nil {
		return err
	}
	if _, err := c.ensureSigil("agent-bob", bob); err != nil {
		return err
	}
	if _, err := c.ensureSigil("agent-carol", carol); err != nil {
		return err
	}
	fmt.Println("setup: sigils registered for alice, bob, carol")

	// alice's ephemeral session key (persisted in the keyring so the later
	// cycle-and-assert process can load it), funded with CLV for bond + gas.
	sessPriv, err := c.ensureKey(aliceSession)
	if err != nil {
		return err
	}
	sessAddr := addrOf(sessPriv)
	if err := c.deliver(alice, &banktypes.MsgSend{
		FromAddress: addrOf(alice).String(),
		ToAddress:   sessAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(uclv, 100*oneCLV)),
	}); err != nil {
		return fmt.Errorf("fund alice-session: %w", err)
	}

	// provision a FRESH solver key (NOT the genesis-bonded solver1), funded from
	// the faucet, so conclave-solver exercises MsgRegisterSolver + bonding end to
	// end (§1.3 step 5). 200 CLV covers the 100 CLV bond plus gas.
	solverPriv, err := c.ensureKey("solver2")
	if err != nil {
		return err
	}
	faucet, err := c.key("faucet")
	if err != nil {
		return err
	}
	if err := c.deliver(faucet, &banktypes.MsgSend{
		FromAddress: addrOf(faucet).String(),
		ToAddress:   addrOf(solverPriv).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(uclv, 200*oneCLV)),
	}); err != nil {
		return fmt.Errorf("fund solver2: %w", err)
	}
	fmt.Printf("setup: funded fresh solver key solver2 (%s)\n", addrOf(solverPriv))

	// step 2: alice issues a Mandate to the session key — allowed msgs =
	// [MsgSubmitIntent, MsgCancelIntent], 500 USDC/tx + window, +10 CLV to
	// budget intent bonds (D-025), expiry +7 days. Replaces any prior mandate.
	height, err := c.height()
	if err != nil {
		return err
	}
	ceiling := sdk.NewCoins(sdk.NewInt64Coin(uusdc, 500*usdc), sdk.NewInt64Coin(uclv, 10*oneCLV))
	if err := c.deliver(alice, &mandatetypes.MsgIssueMandate{
		Controller: addrOf(alice).String(),
		Terms: mandatetypes.MandateTerms{
			SigilId:            aliceSigil,
			GranteeAddr:        sessAddr.String(),
			AllowedMsgTypes:    []string{sdk.MsgTypeURL(&veiltypes.MsgSubmitIntent{}), sdk.MsgTypeURL(&veiltypes.MsgCancelIntent{})},
			SpendCeilingPerTx:  ceiling,
			SpendCeilingWindow: ceiling,
			WindowBlocks:       86_400,
			FeeDenoms:          []string{uusdc},
			ExpiryHeight:       height + 7*86_400,
		},
	}); err != nil {
		return fmt.Errorf("issue mandate: %w", err)
	}
	fmt.Println("setup: mandate issued to alice-session (500 USDC/day ceiling)")

	// step 3: an over-ceiling intent (600 USDC give) must be REJECTED in the
	// ante handler with ErrMandateExceeded — caught in CheckTx (no inclusion).
	overIntent := c.buildIntent(sessAddr, aliceSigil,
		[]veiltypes.IntentLeg{coinLeg(uusdc, 600*usdc)},
		[]veiltypes.IntentLeg{dataLeg(datasetTag)}, height+settleTTL)
	code, _, err := c.broadcastCheck(sessPriv, overIntent)
	if err != nil {
		return fmt.Errorf("over-ceiling broadcast: %w", err)
	}
	if code != mandatetypes.ErrMandateExceeded.ABCICode() {
		return fmt.Errorf("§1.3 step 3: over-ceiling intent got CheckTx code %d, want ErrMandateExceeded (%d)",
			code, mandatetypes.ErrMandateExceeded.ABCICode())
	}
	fmt.Println("setup: over-ceiling intent rejected with ErrMandateExceeded (SPEC §1.3 step 3) ✓")
	fmt.Println("SETUP OK")
	return nil
}

// ---- §1.3 steps 4-6 ---------------------------------------------------------

func (c *cli) cycleAndAssert() error {
	alice, err := c.key("alice")
	if err != nil {
		return err
	}
	bob, err := c.key("bob")
	if err != nil {
		return err
	}
	carol, err := c.key("carol")
	if err != nil {
		return err
	}
	sessPriv, err := c.key(aliceSession)
	if err != nil {
		return err
	}
	aliceSigil := c.mustSigil(addrOf(alice))
	bobSigil := c.mustSigil(addrOf(bob))
	carolSigil := c.mustSigil(addrOf(carol))

	height, err := c.height()
	if err != nil {
		return err
	}
	ttl := height + settleTTL

	// step 4: the 3-party cycle — alice 100 USDC → dataset D; bob D → 10 cmpt;
	// carol 10 cmpt → 100 USDC. Submitted concurrently so they seal into ONE
	// session (same submit block).
	specs := []txSpec{
		{priv: sessPriv, msg: c.buildIntent(addrOf(sessPriv), aliceSigil,
			[]veiltypes.IntentLeg{coinLeg(uusdc, 100*usdc)}, []veiltypes.IntentLeg{dataLeg(datasetTag)}, ttl)},
		{priv: bob, msg: c.buildIntent(addrOf(bob), bobSigil,
			[]veiltypes.IntentLeg{dataLeg(datasetTag)}, []veiltypes.IntentLeg{svcLeg(svcSchema, 10)}, ttl)},
		{priv: carol, msg: c.buildIntent(addrOf(carol), carolSigil,
			[]veiltypes.IntentLeg{svcLeg(svcSchema, 10)}, []veiltypes.IntentLeg{coinLeg(uusdc, 100*usdc)}, ttl)},
	}
	ids, err := c.submitConcurrent(specs)
	if err != nil {
		return fmt.Errorf("submit cycle: %w", err)
	}
	fmt.Printf("cycle: submitted 3 cyclic intents %v\n", short(ids))

	if err := c.waitSameSession(ids); err != nil {
		return err
	}

	// steps 5-6: the running solver registers/bonds, finds the cycle, and
	// submits it; one Conclude settles the cycle. Wait for all CONCLUDED.
	if err := c.waitState(ids, veiltypes.INTENT_STATE_CONCLUDED); err != nil {
		return fmt.Errorf("waiting for the solver to clear the cycle: %w", err)
	}
	fmt.Println("cycle: all three intents CONCLUDED (cleared by the solver's cycle finder) ✓")

	// step 6: receipts queryable per party; alice's reputation incremented.
	for _, s := range []struct {
		name, id string
	}{{"alice", aliceSigil}, {"bob", bobSigil}, {"carol", carolSigil}} {
		n, err := c.receiptCount(s.id)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("no receipt for %s (sigil %s)", s.name, short1(s.id))
		}
		fmt.Printf("cycle: %s has %d receipt(s)\n", s.name, n)
	}
	rep, err := c.reputation(aliceSigil)
	if err != nil {
		return err
	}
	if !rep.IsPositive() {
		return fmt.Errorf("alice reputation did not increment (score %s)", rep)
	}
	fmt.Printf("cycle: alice reputation incremented to %s ✓\n", rep)
	fmt.Println("CYCLE CLEARED OK")
	return nil
}

// ---- §1.3 step 7 ------------------------------------------------------------

func (c *cli) fallback() error {
	bob, err := c.key("bob")
	if err != nil {
		return err
	}
	carol, err := c.key("carol")
	if err != nil {
		return err
	}
	sessPriv, err := c.key(aliceSession)
	if err != nil {
		return err
	}
	alice, err := c.key("alice")
	if err != nil {
		return err
	}
	aliceSigil := c.mustSigil(addrOf(alice))
	bobSigil := c.mustSigil(addrOf(bob))
	carolSigil := c.mustSigil(addrOf(carol))

	height, err := c.height()
	if err != nil {
		return err
	}
	ttl := height + settleTTL

	// 7a: with the solver dead, a mutually-fillable 2-party swap (bob 50 USDC ↔
	// carol 5 cmpt) is cleared by the IN-CONSENSUS fallback matcher (liveness).
	swap := []txSpec{
		{priv: bob, msg: c.buildIntent(addrOf(bob), bobSigil,
			[]veiltypes.IntentLeg{coinLeg(uusdc, 50*usdc)}, []veiltypes.IntentLeg{svcLeg(svcSchema, 5)}, ttl)},
		{priv: carol, msg: c.buildIntent(addrOf(carol), carolSigil,
			[]veiltypes.IntentLeg{svcLeg(svcSchema, 5)}, []veiltypes.IntentLeg{coinLeg(uusdc, 50*usdc)}, ttl)},
	}
	swapIDs, err := c.submitConcurrent(swap)
	if err != nil {
		return fmt.Errorf("submit swap: %w", err)
	}
	fmt.Printf("fallback: submitted 2-party swap %v (solver is dead)\n", short(swapIDs))
	if err := c.waitState(swapIDs, veiltypes.INTENT_STATE_CONCLUDED); err != nil {
		return fmt.Errorf("fallback did not clear the 2-party swap: %w", err)
	}
	if err := c.assertNoSolverSolution(swapIDs[0]); err != nil {
		return err
	}
	fmt.Println("fallback: 2-party swap cleared by the in-consensus fallback (no solver solution) ✓")

	// 7b: a fresh 3-party cycle with the solver dead must NOT clear — the
	// pairwise fallback cannot pair a cycle. Proves solver/cycles is
	// load-bearing (no solver ⇒ cycle never CONCLUDED).
	height, err = c.height()
	if err != nil {
		return err
	}
	ttl = height + settleTTL
	cyc := []txSpec{
		{priv: sessPriv, msg: c.buildIntent(addrOf(sessPriv), aliceSigil,
			[]veiltypes.IntentLeg{coinLeg(uusdc, 100*usdc)}, []veiltypes.IntentLeg{dataLeg(datasetTag)}, ttl)},
		{priv: bob, msg: c.buildIntent(addrOf(bob), bobSigil,
			[]veiltypes.IntentLeg{dataLeg(datasetTag)}, []veiltypes.IntentLeg{svcLeg(svcSchema, 10)}, ttl)},
		{priv: carol, msg: c.buildIntent(addrOf(carol), carolSigil,
			[]veiltypes.IntentLeg{svcLeg(svcSchema, 10)}, []veiltypes.IntentLeg{coinLeg(uusdc, 100*usdc)}, ttl)},
	}
	cycIDs, err := c.submitConcurrent(cyc)
	if err != nil {
		return fmt.Errorf("submit dead-cycle: %w", err)
	}
	fmt.Printf("fallback: submitted 3-party cycle %v with no solver — must NOT clear\n", short(cycIDs))
	if err := c.assertNotConcludedFor(cycIDs, 12); err != nil {
		return err
	}
	fmt.Println("fallback: the 3-party cycle stayed unmatched without a solver (fallback is pairwise-only) ✓")
	fmt.Println("FALLBACK OK")
	return nil
}

// ---- helpers ----------------------------------------------------------------

type txSpec struct {
	priv cryptotypes.PrivKey
	msg  sdk.Msg
}

func (c *cli) buildIntent(sender sdk.AccAddress, sigil string, give, want []veiltypes.IntentLeg, ttl uint64) *veiltypes.MsgSubmitIntent {
	payload := veiltypes.IntentPayload{Give: give, Want: want}
	payloadBz, err := payload.Marshal()
	if err != nil {
		panic(err)
	}
	// declare spend_commitment = the coin give legs (the honest declaration,
	// D-065). Without it the ante values the intent at bond-only — so the
	// §1.3-step-3 over-ceiling reject never fires — and the Reveal slashes
	// every coin-give intent INVALID (actual give > the empty commitment).
	// This builder predates D-065 and was stale until task 2.3 (D-083).
	commitment := sdk.NewCoins()
	for _, leg := range give {
		if coin := leg.GetCoin(); coin != nil {
			commitment = commitment.Add(*coin)
		}
	}
	envBz, err := (&veiltypes.IntentEnvelope{TtlHeight: ttl, SpendCommitment: commitment}).Marshal()
	if err != nil {
		panic(err)
	}
	return &veiltypes.MsgSubmitIntent{
		Sender:      sender.String(),
		SenderSigil: sigil,
		Ciphertext:  payloadBz,
		Envelope:    envBz,
		Bond:        sdk.NewInt64Coin(uclv, oneCLV),
	}
}

func coinLeg(denom string, amt int64) veiltypes.IntentLeg {
	c := sdk.NewInt64Coin(denom, amt)
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Coin{Coin: &c}}
}

func svcLeg(schema string, units int64) veiltypes.IntentLeg {
	v := veiltypes.ServiceVoucher{Schema: schema, Units: math.NewInt(units)}
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Svc{Svc: &v}}
}

func dataLeg(tag string) veiltypes.IntentLeg {
	h := sha256.Sum256([]byte(tag))
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Data{Data: &veiltypes.DataGrant{ContentHash: h[:]}}}
}

func (c *cli) ensureSigil(tag string, priv cryptotypes.PrivKey) (string, error) {
	addr := addrOf(priv)
	if res, err := c.sigilc.SigilByAddress(context.Background(),
		&sigiltypes.QuerySigilByAddressRequest{Address: addr.String()}); err == nil {
		return res.Sigil.Id, nil
	}
	did := sha256.Sum256([]byte("did:conclave:" + tag))
	if err := c.deliver(priv, &sigiltypes.MsgRegisterSigil{
		Controller:  addr.String(),
		DidDocHash:  did[:],
		MetadataUri: "ipfs://" + tag,
	}); err != nil {
		return "", fmt.Errorf("register sigil %s: %w", tag, err)
	}
	res, err := c.sigilc.SigilByAddress(context.Background(),
		&sigiltypes.QuerySigilByAddressRequest{Address: addr.String()})
	if err != nil {
		return "", err
	}
	return res.Sigil.Id, nil
}

func (c *cli) mustSigil(addr sdk.AccAddress) string {
	res, err := c.sigilc.SigilByAddress(context.Background(),
		&sigiltypes.QuerySigilByAddressRequest{Address: addr.String()})
	if err != nil {
		fail(fmt.Errorf("no sigil for %s — run setup first: %w", addr, err))
	}
	return res.Sigil.Id
}

func (c *cli) reputation(sigil string) (math.LegacyDec, error) {
	res, err := c.sigilc.Reputation(context.Background(), &sigiltypes.QueryReputationRequest{SigilId: sigil})
	if err != nil {
		return math.LegacyDec{}, err
	}
	return res.Reputation.Score, nil
}

func (c *cli) receiptCount(sigil string) (int, error) {
	res, err := c.receiptc.Receipts(context.Background(), &receipttypes.QueryReceiptsRequest{SigilId: sigil})
	if err != nil {
		return 0, err
	}
	return len(res.Receipts), nil
}

// submitConcurrent broadcasts each spec from its own key concurrently (so they
// land in one block), requires CheckTx code 0, waits for inclusion, and returns
// the assigned intent ids in spec order.
func (c *cli) submitConcurrent(specs []txSpec) ([]string, error) {
	type signed struct {
		bz   []byte
		hash string
	}
	out := make([]signed, len(specs))
	for i, s := range specs {
		acc, err := c.account(addrOf(s.priv))
		if err != nil {
			return nil, err
		}
		bz, err := c.signTx(s.priv, acc.GetAccountNumber(), acc.GetSequence(), s.msg)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(bz)
		out[i] = signed{bz: bz, hash: strings.ToUpper(hex.EncodeToString(sum[:]))}
	}

	var wg sync.WaitGroup
	codes := make([]uint32, len(specs))
	errs := make([]error, len(specs))
	for i := range out {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := c.txc.BroadcastTx(context.Background(), &txtypes.BroadcastTxRequest{
				TxBytes: out[i].bz, Mode: txtypes.BroadcastMode_BROADCAST_MODE_SYNC,
			})
			if err != nil {
				errs[i] = err
				return
			}
			codes[i] = res.TxResponse.Code
		}(i)
	}
	wg.Wait()
	for i := range specs {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if codes[i] != 0 {
			return nil, fmt.Errorf("intent %d rejected in CheckTx with code %d", i, codes[i])
		}
	}

	ids := make([]string, len(specs))
	for i := range out {
		id, err := c.intentIDForTx(out[i].hash)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

// intentIDForTx waits for the tx to be included and extracts the assigned intent
// id from the MsgSubmitIntentResponse.
func (c *cli) intentIDForTx(hash string) (string, error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		res, err := c.txc.GetTx(context.Background(), &txtypes.GetTxRequest{Hash: hash})
		if err == nil && res.TxResponse.Height > 0 {
			if res.TxResponse.Code != 0 {
				return "", fmt.Errorf("submit tx %s failed: code %d: %s", hash, res.TxResponse.Code, res.TxResponse.RawLog)
			}
			for _, ev := range res.TxResponse.Events {
				if ev.Type == "conclave.veil.v1.EventIntentSubmitted" || strings.Contains(ev.Type, "IntentSubmitted") {
					for _, a := range ev.Attributes {
						if a.Key == "intent_id" {
							return strings.Trim(a.Value, "\""), nil
						}
					}
				}
			}
			// fall back to decoding the msg response
			if id := c.intentIDFromMsgResponse(res); id != "" {
				return id, nil
			}
			return "", fmt.Errorf("tx %s: no intent_id in response", hash)
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("submit tx %s not included before deadline", hash)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (c *cli) intentIDFromMsgResponse(res *txtypes.GetTxResponse) string {
	if res.TxResponse == nil || len(res.TxResponse.Data) == 0 {
		return ""
	}
	dataBz, err := hex.DecodeString(res.TxResponse.Data)
	if err != nil {
		return ""
	}
	var txMsgData sdk.TxMsgData
	if err := c.conclave.AppCodec().Unmarshal(dataBz, &txMsgData); err != nil {
		return ""
	}
	for _, m := range txMsgData.MsgResponses {
		var r veiltypes.MsgSubmitIntentResponse
		if err := c.conclave.AppCodec().Unmarshal(m.Value, &r); err == nil && r.IntentId != "" {
			return r.IntentId
		}
	}
	return ""
}

func (c *cli) intentRecord(id string) (veiltypes.IntentRecord, error) {
	res, err := c.veilc.Intent(context.Background(), &veiltypes.QueryIntentRequest{Id: id})
	if err != nil {
		return veiltypes.IntentRecord{}, err
	}
	return res.Record, nil
}

// waitSameSession blocks until every intent is OPEN (revealed) and asserts they
// all sealed into the same session — the precondition for clearing them in one
// match.
func (c *cli) waitSameSession(ids []string) error {
	deadline := time.Now().Add(waitTimeout)
	for {
		allOpen := true
		session := uint64(0)
		ok := true
		for i, id := range ids {
			rec, err := c.intentRecord(id)
			if err != nil {
				return err
			}
			if rec.State != veiltypes.INTENT_STATE_OPEN && rec.State != veiltypes.INTENT_STATE_CONCLUDED {
				allOpen = false
				break
			}
			if i == 0 {
				session = rec.SessionId
			} else if rec.SessionId != session {
				ok = false
			}
		}
		if allOpen {
			if !ok {
				return fmt.Errorf("intents did not seal into one session (concurrent submit split across a boundary); re-run")
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("intents did not all reach OPEN before deadline")
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func (c *cli) waitState(ids []string, want veiltypes.IntentState) error {
	deadline := time.Now().Add(waitTimeout)
	for {
		all := true
		for _, id := range ids {
			rec, err := c.intentRecord(id)
			if err != nil {
				return err
			}
			if rec.State != want {
				all = false
				break
			}
		}
		if all {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("intents did not all reach %s before deadline", want)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// assertNoSolverSolution confirms the session that cleared an intent carried NO
// solver solution — i.e. it was cleared by the in-consensus fallback.
func (c *cli) assertNoSolverSolution(intentID string) error {
	rec, err := c.intentRecord(intentID)
	if err != nil {
		return err
	}
	res, err := c.sessionc.Solutions(context.Background(), &sessiontypes.QuerySolutionsRequest{SessionId: rec.SessionId})
	if err != nil {
		return err
	}
	if len(res.Solutions) != 0 {
		return fmt.Errorf("session %d had %d solver solution(s); expected fallback (zero)", rec.SessionId, len(res.Solutions))
	}
	return nil
}

// assertNotConcludedFor polls for the given number of blocks and fails if any
// intent reaches CONCLUDED (the dead-cycle must never clear without a solver).
func (c *cli) assertNotConcludedFor(ids []string, blocks int) error {
	start, err := c.height()
	if err != nil {
		return err
	}
	for {
		for _, id := range ids {
			rec, err := c.intentRecord(id)
			if err != nil {
				return err
			}
			if rec.State == veiltypes.INTENT_STATE_CONCLUDED {
				return fmt.Errorf("intent %s CONCLUDED without a solver — fallback must not clear a cycle", short1(id))
			}
		}
		h, err := c.height()
		if err != nil {
			return err
		}
		if h >= start+uint64(blocks) {
			return nil
		}
		time.Sleep(400 * time.Millisecond)
	}
}

// ---- key + tx plumbing (mirrors cmd/unordered-blast) ------------------------

func (c *cli) key(name string) (cryptotypes.PrivKey, error) {
	armor, err := c.kr.ExportPrivKeyArmor(name, "demo")
	if err != nil {
		return nil, fmt.Errorf("export key %q: %w", name, err)
	}
	priv, _, err := crypto.UnarmorDecryptPrivKey(armor, "demo")
	return priv, err
}

func (c *cli) ensureKey(name string) (cryptotypes.PrivKey, error) {
	if priv, err := c.key(name); err == nil {
		return priv, nil
	}
	if _, _, err := c.kr.NewMnemonic(name, keyring.English, sdk.FullFundraiserPath, keyring.DefaultBIP39Passphrase, hd.Secp256k1); err != nil {
		return nil, fmt.Errorf("create key %q: %w", name, err)
	}
	return c.key(name)
}

func (c *cli) deliver(priv cryptotypes.PrivKey, msgs ...sdk.Msg) error {
	acc, err := c.account(addrOf(priv))
	if err != nil {
		return err
	}
	bz, err := c.signTx(priv, acc.GetAccountNumber(), acc.GetSequence(), msgs...)
	if err != nil {
		return err
	}
	res, err := c.txc.BroadcastTx(context.Background(), &txtypes.BroadcastTxRequest{
		TxBytes: bz, Mode: txtypes.BroadcastMode_BROADCAST_MODE_SYNC,
	})
	if err != nil {
		return err
	}
	if res.TxResponse.Code != 0 {
		return fmt.Errorf("CheckTx code %d: %s", res.TxResponse.Code, res.TxResponse.RawLog)
	}
	sum := sha256.Sum256(bz)
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	deadline := time.Now().Add(30 * time.Second)
	for {
		got, err := c.txc.GetTx(context.Background(), &txtypes.GetTxRequest{Hash: hash})
		if err == nil && got.TxResponse.Height > 0 {
			if got.TxResponse.Code != 0 {
				return fmt.Errorf("tx %s failed in DeliverTx: code %d: %s", hash, got.TxResponse.Code, got.TxResponse.RawLog)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("tx %s not included before deadline", hash)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// broadcastCheck broadcasts in SYNC mode and returns the CheckTx code/log
// without waiting for inclusion (used for the ante-rejection assertion).
func (c *cli) broadcastCheck(priv cryptotypes.PrivKey, msgs ...sdk.Msg) (uint32, string, error) {
	acc, err := c.account(addrOf(priv))
	if err != nil {
		return 0, "", err
	}
	bz, err := c.signTx(priv, acc.GetAccountNumber(), acc.GetSequence(), msgs...)
	if err != nil {
		return 0, "", err
	}
	res, err := c.txc.BroadcastTx(context.Background(), &txtypes.BroadcastTxRequest{
		TxBytes: bz, Mode: txtypes.BroadcastMode_BROADCAST_MODE_SYNC,
	})
	if err != nil {
		return 0, "", err
	}
	return res.TxResponse.Code, res.TxResponse.RawLog, nil
}

func (c *cli) signTx(priv cryptotypes.PrivKey, accNum, seq uint64, msgs ...sdk.Msg) ([]byte, error) {
	return c.signTxFee(priv, accNum, seq, nil, msgs...)
}

// signTxFee is signTx with an explicit fee — the x402-check §4 case needs a
// real uusdc fee on the wire for the FeeAbstractionDecorator to convert.
func (c *cli) signTxFee(priv cryptotypes.PrivKey, accNum, seq uint64, fee sdk.Coins, msgs ...sdk.Msg) ([]byte, error) {
	txConfig := c.conclave.TxConfig()
	builder := txConfig.NewTxBuilder()
	if err := builder.SetMsgs(msgs...); err != nil {
		return nil, err
	}
	builder.SetGasLimit(2_000_000)
	if !fee.IsZero() {
		builder.SetFeeAmount(fee)
	}

	addr := addrOf(priv)
	if err := builder.SetSignatures(signingtypes.SignatureV2{
		PubKey:   priv.PubKey(),
		Data:     &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
		Sequence: seq,
	}); err != nil {
		return nil, err
	}
	sig, err := clienttx.SignWithPrivKey(
		context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT,
		authsigning.SignerData{
			Address:       addr.String(),
			ChainID:       c.chainID,
			AccountNumber: accNum,
			Sequence:      seq,
			PubKey:        priv.PubKey(),
		},
		builder, priv, txConfig, seq,
	)
	if err != nil {
		return nil, err
	}
	if err := builder.SetSignatures(sig); err != nil {
		return nil, err
	}
	return txConfig.TxEncoder()(builder.GetTx())
}

func (c *cli) account(addr sdk.AccAddress) (sdk.AccountI, error) {
	res, err := c.authc.Account(context.Background(), &authtypes.QueryAccountRequest{Address: addr.String()})
	if err != nil {
		return nil, fmt.Errorf("account %s: %w", addr, err)
	}
	var acc sdk.AccountI
	if err := c.conclave.InterfaceRegistry().UnpackAny(res.Account, &acc); err != nil {
		return nil, err
	}
	return acc, nil
}

func (c *cli) height() (uint64, error) {
	res, err := c.cmtc.GetLatestBlock(context.Background(), &cmtservice.GetLatestBlockRequest{})
	if err != nil {
		return 0, err
	}
	return uint64(res.SdkBlock.Header.Height), nil
}

func addrOf(priv cryptotypes.PrivKey) sdk.AccAddress {
	return sdk.AccAddress(priv.PubKey().Address())
}

func short(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = short1(id)
	}
	return out
}

func short1(id string) string {
	if len(id) > 10 {
		return id[:10]
	}
	return id
}
