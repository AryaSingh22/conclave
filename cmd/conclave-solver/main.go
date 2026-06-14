// Command conclave-solver is the SPEC §4 / §10 row-0.10 reference solver: an
// external, permissionless gRPC client that observes open Sessions, computes a
// matching (cycles + greedy pairwise), and submits MsgProposeSolution after
// registering and bonding (SPEC §6.4). It is NOT consensus code — its output is
// validated by the on-chain 6-rule check; this binary's job is to propose
// solutions that PASS that check (and never one it would be slashed for).
//
// It is a CLIENT of the node's gRPC, reusing the signing/broadcast pattern of
// cmd/unordered-blast (task 0.5). Run it against a localnet; kill it to exercise
// the in-consensus fallback (SPEC §1.3 step 7).
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log"

	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/AryaSingh22/conclave/app"
	"github.com/AryaSingh22/conclave/solver"
	"github.com/AryaSingh22/conclave/solver/match"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

func main() {
	var (
		grpcAddr = flag.String("grpc", "localhost:9090", "node gRPC endpoint")
		home     = flag.String("home", os.ExpandEnv("$HOME/.conclaved"), "node home (test keyring)")
		from     = flag.String("from", "solver1", "key name in the test keyring")
		chainID  = flag.String("chain-id", "conclave-dev-1", "chain id")
		bondStr  = flag.String("bond", "100000000uclv", "solver bond (>= solver_min_bond) if not already registered")
		chamber  = flag.Uint("chamber", 0, "chamber id (fixed 0 in Phase 0)")
		poll     = flag.Duration("poll", 500*time.Millisecond, "session poll interval")
		scan     = flag.Uint("scan", 64, "how many session ids below the current one to scan for AUCTION")
		once     = flag.Bool("once", false, "solve a single pass and exit (testing)")
	)
	flag.Parse()

	bond, err := sdk.ParseCoinsNormalized(*bondStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: bad -bond:", err)
		os.Exit(1)
	}
	cfg := config{
		grpcAddr: *grpcAddr, home: *home, from: *from, chainID: *chainID,
		bond: bond, chamber: uint32(*chamber), poll: *poll, scan: uint64(*scan), once: *once,
	}
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

type config struct {
	grpcAddr, home, from, chainID string
	bond                          sdk.Coins
	chamber                       uint32
	poll                          time.Duration
	scan                          uint64
	once                          bool
}

type client struct {
	cfg      config
	conclave *app.ConclaveApp // throwaway: tx encoding + interface registry only
	conn     *grpc.ClientConn
	txc      txtypes.ServiceClient
	authc    authtypes.QueryClient
	sessionc sessiontypes.QueryClient
	veilc    veiltypes.QueryClient
	priv     cryptotypes.PrivKey
	addr     sdk.AccAddress
}

func run(cfg config) error {
	conn, err := grpc.NewClient(cfg.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	conclave := app.NewConclaveApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true, sims.EmptyAppOptions{})
	kr, err := keyring.New("conclave", keyring.BackendTest, cfg.home, nil, conclave.AppCodec())
	if err != nil {
		return err
	}
	armor, err := kr.ExportPrivKeyArmor(cfg.from, "solver")
	if err != nil {
		return fmt.Errorf("export key %q: %w", cfg.from, err)
	}
	priv, _, err := crypto.UnarmorDecryptPrivKey(armor, "solver")
	if err != nil {
		return err
	}

	cl := &client{
		cfg:      cfg,
		conclave: conclave,
		conn:     conn,
		txc:      txtypes.NewServiceClient(conn),
		authc:    authtypes.NewQueryClient(conn),
		sessionc: sessiontypes.NewQueryClient(conn),
		veilc:    veiltypes.NewQueryClient(conn),
		priv:     priv,
		addr:     sdk.AccAddress(priv.PubKey().Address()),
	}

	if err := cl.ensureBonded(); err != nil {
		return fmt.Errorf("ensure bonded: %w", err)
	}

	if cfg.once {
		return cl.solvePass()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	fmt.Printf("conclave-solver %s polling %s every %s\n", cl.addr, cfg.grpcAddr, cfg.poll)
	ticker := time.NewTicker(cfg.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Println("conclave-solver: shutting down")
			return nil
		case <-ticker.C:
			if err := cl.solvePass(); err != nil {
				fmt.Fprintln(os.Stderr, "warn: solve pass:", err)
			}
		}
	}
}

// ensureBonded registers + bonds the solver if it is not already ACTIVE (SPEC
// §6.4). Idempotent: a solver already in the registry (e.g. genesis solver1) is
// left as-is, so re-running the binary never double-registers.
func (cl *client) ensureBonded() error {
	res, err := cl.sessionc.Solver(context.Background(), &sessiontypes.QuerySolverRequest{Addr: cl.addr.String()})
	if err == nil && res.Solver.Status == sessiontypes.SOLVER_STATUS_ACTIVE {
		fmt.Printf("conclave-solver %s already ACTIVE (bond %s)\n", cl.addr, res.Solver.Bond)
		return nil
	}
	fmt.Printf("conclave-solver %s registering with bond %s\n", cl.addr, cl.cfg.bond)
	if err := cl.broadcastWait(&sessiontypes.MsgRegisterSolver{
		Solver: cl.addr.String(),
		Bond:   cl.cfg.bond,
	}); err != nil {
		return err
	}
	return nil
}

// solvePass scans recent sessions for any in AUCTION that this solver has not
// already solved, computes a matching, and submits it.
func (cl *client) solvePass() error {
	cur, err := cl.sessionc.CurrentSession(context.Background(),
		&sessiontypes.QueryCurrentSessionRequest{ChamberId: cl.cfg.chamber})
	if err != nil {
		return nil // no session yet
	}
	top := cur.Session.Id

	// scan downward from the current id: CurrentSession returns the highest
	// non-terminal session, which may be a newer FORMING session masking an
	// in-flight AUCTION one. Session ids are boundary heights (D-038), so most
	// scanned ids are NotFound — skipped.
	for id := top; id+cl.cfg.scan >= top && id > 0; id-- {
		var s sessiontypes.Session
		if id == top {
			s = cur.Session
		} else {
			r, err := cl.sessionc.Session(context.Background(), &sessiontypes.QuerySessionRequest{Id: id})
			if err != nil {
				continue
			}
			s = r.Session
		}
		if s.State != sessiontypes.SESSION_STATE_AUCTION {
			continue
		}
		if err := cl.solveSession(s); err != nil {
			fmt.Fprintf(os.Stderr, "warn: session %d: %v\n", id, err)
		}
	}
	return nil
}

// solveSession solves one AUCTION session: skip if already solved by us
// (equivocation is a 20% slash), decode its OPEN intents, run the matcher, and
// submit the solution if anything clears.
func (cl *client) solveSession(s sessiontypes.Session) error {
	sols, err := cl.sessionc.Solutions(context.Background(), &sessiontypes.QuerySolutionsRequest{SessionId: s.Id})
	if err == nil {
		for _, sol := range sols.Solutions {
			if sol.Solver == cl.addr.String() {
				return nil // already submitted — never equivocate
			}
		}
	}

	intents, err := cl.openIntents(s)
	if err != nil {
		return err
	}
	matches := solver.Solve(intents)
	if len(matches) == 0 {
		return nil // nothing clears this pass
	}

	fmt.Printf("conclave-solver: session %d — proposing %d match(es) over %d OPEN intents\n",
		s.Id, len(matches), len(intents))
	return cl.broadcastWait(&sessiontypes.MsgProposeSolution{
		Solver:    cl.addr.String(),
		SessionId: s.Id,
		Matches:   matches,
	})
}

// openIntents fetches the session's OPEN intents and decodes them into the
// matcher's view. Filters the veil OPEN set to this session's intent_ids.
func (cl *client) openIntents(s sessiontypes.Session) ([]match.OpenIntent, error) {
	inSession := make(map[string]bool, len(s.IntentIds))
	for _, id := range s.IntentIds {
		inSession[id] = true
	}
	res, err := cl.veilc.Intents(context.Background(), &veiltypes.QueryIntentsRequest{
		State: veiltypes.INTENT_STATE_OPEN,
	})
	if err != nil {
		return nil, err
	}
	var out []match.OpenIntent
	for i := range res.Records {
		rec := res.Records[i]
		if !inSession[rec.Intent.Id] {
			continue
		}
		if oi, ok := match.DecodeOpenIntent(rec); ok {
			out = append(out, oi)
		}
	}
	return out, nil
}

// broadcastWait signs an ordered tx at the account's true sequence, broadcasts
// it in SYNC mode, and waits for inclusion (erroring on a non-zero code).
func (cl *client) broadcastWait(msgs ...sdk.Msg) error {
	acc, err := cl.account(cl.addr)
	if err != nil {
		return err
	}
	bz, err := cl.signTx(acc.GetAccountNumber(), acc.GetSequence(), msgs...)
	if err != nil {
		return err
	}
	res, err := cl.txc.BroadcastTx(context.Background(), &txtypes.BroadcastTxRequest{
		TxBytes: bz, Mode: txtypes.BroadcastMode_BROADCAST_MODE_SYNC,
	})
	if err != nil {
		return err
	}
	if res.TxResponse.Code != 0 {
		return fmt.Errorf("CheckTx code %d: %s", res.TxResponse.Code, res.TxResponse.RawLog)
	}
	sum := sha256.Sum256(bz)
	return cl.awaitInclusion(strings.ToUpper(hex.EncodeToString(sum[:])))
}

// signTx signs an ordered tx (SIGN_MODE_DIRECT) — mirrors the conformance/blast
// signer (cmd/unordered-blast).
func (cl *client) signTx(accNum, seq uint64, msgs ...sdk.Msg) ([]byte, error) {
	txConfig := cl.conclave.TxConfig()
	builder := txConfig.NewTxBuilder()
	if err := builder.SetMsgs(msgs...); err != nil {
		return nil, err
	}
	builder.SetGasLimit(2_000_000)

	if err := builder.SetSignatures(signingtypes.SignatureV2{
		PubKey:   cl.priv.PubKey(),
		Data:     &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
		Sequence: seq,
	}); err != nil {
		return nil, err
	}
	sig, err := clienttx.SignWithPrivKey(
		context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT,
		authsigning.SignerData{
			Address:       cl.addr.String(),
			ChainID:       cl.cfg.chainID,
			AccountNumber: accNum,
			Sequence:      seq,
			PubKey:        cl.priv.PubKey(),
		},
		builder, cl.priv, txConfig, seq,
	)
	if err != nil {
		return nil, err
	}
	if err := builder.SetSignatures(sig); err != nil {
		return nil, err
	}
	return txConfig.TxEncoder()(builder.GetTx())
}

func (cl *client) account(addr sdk.AccAddress) (sdk.AccountI, error) {
	res, err := cl.authc.Account(context.Background(), &authtypes.QueryAccountRequest{Address: addr.String()})
	if err != nil {
		return nil, fmt.Errorf("account %s: %w", addr, err)
	}
	var acc sdk.AccountI
	if err := cl.conclave.InterfaceRegistry().UnpackAny(res.Account, &acc); err != nil {
		return nil, err
	}
	return acc, nil
}

func (cl *client) awaitInclusion(hash string) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		res, err := cl.txc.GetTx(context.Background(), &txtypes.GetTxRequest{Hash: hash})
		if err == nil && res.TxResponse.Height > 0 {
			if res.TxResponse.Code != 0 {
				return fmt.Errorf("tx %s failed in DeliverTx: code %d: %s",
					hash, res.TxResponse.Code, res.TxResponse.RawLog)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("tx %s not included before deadline", hash)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
