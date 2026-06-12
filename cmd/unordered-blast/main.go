// Command unordered-blast is the SPEC §10 row 0.5 localnet demonstration:
// it fires N parallel unordered txs from ONE key — every one with a
// deliberately wrong sequence number — at a running conclaved node and
// asserts they all land in a single block.
//
// With -mandate it runs the 0.4×0.5 intersection instead: the operator
// issues a Mandate to a fresh session key whose window ceiling admits
// exactly K of the N sends; the blast must land exactly the under-ceiling
// prefix while the rest bounce in CheckTx with ErrMandateExceeded.
//
// Dev tooling only — this binary is a gRPC client, not consensus code.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
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
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/AryaSingh22/conclave/app"
	unorderedtypes "github.com/AryaSingh22/conclave/app/unordered/types"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
)

func main() {
	var (
		grpcAddr = flag.String("grpc", "localhost:9090", "node gRPC endpoint")
		home     = flag.String("home", os.ExpandEnv("$HOME/.conclaved"), "node home (test keyring)")
		from     = flag.String("from", "alice", "key name in the test keyring")
		count    = flag.Int("count", 50, "number of unordered txs to blast")
		mandate  = flag.Bool("mandate", false, "run the mandate-intersection variant")
		chainID  = flag.String("chain-id", "conclave-dev-1", "chain id")
	)
	flag.Parse()

	if err := run(*grpcAddr, *home, *from, *chainID, *count, *mandate); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

type blaster struct {
	chainID  string
	conclave *app.ConclaveApp // throwaway: tx encoding + interface registry only
	conn     *grpc.ClientConn
	txc      txtypes.ServiceClient
	authc    authtypes.QueryClient
}

func run(grpcAddr, home, from, chainID string, count int, mandateMode bool) error {
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	b := &blaster{
		chainID:  chainID,
		conclave: app.NewConclaveApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true, sims.EmptyAppOptions{}),
		conn:     conn,
		txc:      txtypes.NewServiceClient(conn),
		authc:    authtypes.NewQueryClient(conn),
	}

	kr, err := keyring.New("conclave", keyring.BackendTest, home, nil, b.conclave.AppCodec())
	if err != nil {
		return err
	}
	armor, err := kr.ExportPrivKeyArmor(from, "blast")
	if err != nil {
		return fmt.Errorf("export key %q: %w", from, err)
	}
	operator, _, err := crypto.UnarmorDecryptPrivKey(armor, "blast")
	if err != nil {
		return err
	}

	if !mandateMode {
		return b.blastSameBlock(operator, count)
	}
	return b.mandateIntersection(operator, count)
}

// blastSameBlock fires count unordered txs with wrong sequences from priv
// and requires every one of them in ONE block.
func (b *blaster) blastSameBlock(priv cryptotypes.PrivKey, count int) error {
	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	send := func(i int) sdk.Msg {
		return &banktypes.MsgSend{
			FromAddress: sdk.AccAddress(priv.PubKey().Address()).String(),
			ToAddress:   recipient.String(),
			Amount:      sdk.NewCoins(sdk.NewCoin(app.BaseDenom, math.NewInt(int64(1+i)))),
		}
	}
	hashes, codes, err := b.broadcastUnordered(priv, count, send)
	if err != nil {
		return err
	}
	for i, code := range codes {
		if code != 0 {
			return fmt.Errorf("tx %d rejected in CheckTx with code %d", i, code)
		}
	}

	heights, err := b.awaitInclusion(hashes)
	if err != nil {
		return err
	}
	distinct := map[int64]int{}
	for _, h := range heights {
		distinct[h]++
	}
	if len(distinct) != 1 {
		return fmt.Errorf("txs spread across %d blocks: %v", len(distinct), distinct)
	}
	for h := range distinct {
		fmt.Printf("PASS: %d unordered txs from one key (wrong sequences) all landed in block %d\n", count, h)
	}
	return nil
}

// mandateIntersection: operator registers a sigil, issues a Mandate to a
// fresh session key admitting exactly `admitted` of the blast, then the
// session key blasts count sends.
func (b *blaster) mandateIntersection(operator cryptotypes.PrivKey, count int) error {
	const sendAmount = 10_000_000 // 10 USDC
	admitted := count / 5         // 10 of 50

	operatorAddr := sdk.AccAddress(operator.PubKey().Address())
	session := secp256k1.GenPrivKey()
	sessionAddr := sdk.AccAddress(session.PubKey().Address())

	// fund the session key (creates its account on chain)
	funding := sdk.NewCoins(
		sdk.NewCoin(app.BaseDenom, math.NewInt(1_000_000_000)),
		sdk.NewCoin(app.MockUSDCDenom, math.NewInt(int64(2*count*sendAmount))),
	)
	if err := b.deliverOrdered(operator, &banktypes.MsgSend{
		FromAddress: operatorAddr.String(), ToAddress: sessionAddr.String(), Amount: funding,
	}); err != nil {
		return fmt.Errorf("fund session key: %w", err)
	}

	// register the operator's sigil if it doesn't exist yet
	sigilc := sigiltypes.NewQueryClient(b.conn)
	sigilRes, err := sigilc.SigilByAddress(context.Background(),
		&sigiltypes.QuerySigilByAddressRequest{Address: operatorAddr.String()})
	var sigilID string
	switch {
	case err == nil:
		sigilID = sigilRes.Sigil.Id
	default:
		didHash := sha256.Sum256([]byte("did:conclave:" + operatorAddr.String()))
		if err := b.deliverOrdered(operator, &sigiltypes.MsgRegisterSigil{
			Controller: operatorAddr.String(),
			DidDocHash: didHash[:],
		}); err != nil {
			return fmt.Errorf("register sigil: %w", err)
		}
		sigilRes, err = sigilc.SigilByAddress(context.Background(),
			&sigiltypes.QuerySigilByAddressRequest{Address: operatorAddr.String()})
		if err != nil {
			return fmt.Errorf("sigil lookup after registration: %w", err)
		}
		sigilID = sigilRes.Sigil.Id
	}

	// issue the Mandate: per-tx = one send, window = `admitted` sends/day
	height, err := b.latestHeight()
	if err != nil {
		return err
	}
	if err := b.deliverOrdered(operator, &mandatetypes.MsgIssueMandate{
		Controller: operatorAddr.String(),
		Terms: mandatetypes.MandateTerms{
			SigilId:            sigilID,
			GranteeAddr:        sessionAddr.String(),
			AllowedMsgTypes:    []string{sdk.MsgTypeURL(&banktypes.MsgSend{})},
			SpendCeilingPerTx:  sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(sendAmount))),
			SpendCeilingWindow: sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(int64(admitted*sendAmount)))),
			WindowBlocks:       86_400,
			FeeDenoms:          []string{app.MockUSDCDenom},
			ExpiryHeight:       uint64(height) + 7*86_400,
		},
	}); err != nil {
		return fmt.Errorf("issue mandate: %w", err)
	}

	// the blast: count unordered sends from the session key
	send := func(int) sdk.Msg {
		return &banktypes.MsgSend{
			FromAddress: sessionAddr.String(), ToAddress: operatorAddr.String(),
			Amount: sdk.NewCoins(sdk.NewCoin(app.MockUSDCDenom, math.NewInt(sendAmount))),
		}
	}
	hashes, codes, err := b.broadcastUnordered(session, count, send)
	if err != nil {
		return err
	}

	var acceptedHashes []string
	bounced := 0
	for i, code := range codes {
		switch code {
		case 0:
			acceptedHashes = append(acceptedHashes, hashes[i])
		case mandatetypes.ErrMandateExceeded.ABCICode():
			bounced++ // rejected in CheckTx by the optimistic window charge
		default:
			return fmt.Errorf("tx %d: unexpected CheckTx code %d", i, code)
		}
	}
	if len(acceptedHashes) != admitted {
		return fmt.Errorf("CheckTx admitted %d txs, want exactly %d", len(acceptedHashes), admitted)
	}

	heights, err := b.awaitInclusion(acceptedHashes)
	if err != nil {
		return err
	}
	distinct := map[int64]bool{}
	for _, h := range heights {
		distinct[h] = true
	}
	fmt.Printf("PASS: mandate intersection — %d/%d unordered txs admitted (window ceiling), "+
		"%d bounced in CheckTx with ErrMandateExceeded, admitted txs in %d block(s)\n",
		len(acceptedHashes), count, bounced, len(distinct))
	return nil
}

// broadcastUnordered signs count unordered txs (wrong sequences 1000+i)
// and broadcasts them concurrently in SYNC mode, returning hashes and
// CheckTx codes.
func (b *blaster) broadcastUnordered(priv cryptotypes.PrivKey, count int, msg func(i int) sdk.Msg) ([]string, []uint32, error) {
	acc, err := b.account(sdk.AccAddress(priv.PubKey().Address()))
	if err != nil {
		return nil, nil, err
	}
	timeout := time.Now().Add(5 * time.Minute)

	txs := make([][]byte, count)
	hashes := make([]string, count)
	for i := 0; i < count; i++ {
		bz, err := b.signTx(priv, acc.GetAccountNumber(), uint64(1000+i), &timeout, msg(i))
		if err != nil {
			return nil, nil, err
		}
		txs[i] = bz
		sum := sha256.Sum256(bz)
		hashes[i] = strings.ToUpper(hex.EncodeToString(sum[:]))
	}

	codes := make([]uint32, count)
	var wg sync.WaitGroup
	errs := make([]error, count)
	for i := range txs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := b.txc.BroadcastTx(context.Background(), &txtypes.BroadcastTxRequest{
				TxBytes: txs[i], Mode: txtypes.BroadcastMode_BROADCAST_MODE_SYNC,
			})
			if err != nil {
				errs[i] = err
				return
			}
			codes[i] = res.TxResponse.Code
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, nil, err
		}
	}
	return hashes, codes, nil
}

// deliverOrdered signs an ordered tx at the account's true sequence and
// waits for inclusion.
func (b *blaster) deliverOrdered(priv cryptotypes.PrivKey, msgs ...sdk.Msg) error {
	acc, err := b.account(sdk.AccAddress(priv.PubKey().Address()))
	if err != nil {
		return err
	}
	bz, err := b.signTx(priv, acc.GetAccountNumber(), acc.GetSequence(), nil, msgs...)
	if err != nil {
		return err
	}
	res, err := b.txc.BroadcastTx(context.Background(), &txtypes.BroadcastTxRequest{
		TxBytes: bz, Mode: txtypes.BroadcastMode_BROADCAST_MODE_SYNC,
	})
	if err != nil {
		return err
	}
	if res.TxResponse.Code != 0 {
		return fmt.Errorf("CheckTx code %d: %s", res.TxResponse.Code, res.TxResponse.RawLog)
	}
	sum := sha256.Sum256(bz)
	heights, err := b.awaitInclusion([]string{strings.ToUpper(hex.EncodeToString(sum[:]))})
	if err != nil {
		return err
	}
	_ = heights
	return nil
}

// signTx mirrors the conformance-suite signer: SIGN_MODE_DIRECT, optional
// unordered extension, arbitrary sequence.
func (b *blaster) signTx(priv cryptotypes.PrivKey, accNum, seq uint64, timeout *time.Time, msgs ...sdk.Msg) ([]byte, error) {
	txConfig := b.conclave.TxConfig()
	builder := txConfig.NewTxBuilder()
	if err := builder.SetMsgs(msgs...); err != nil {
		return nil, err
	}
	builder.SetGasLimit(400_000)

	if timeout != nil {
		ext, err := codectypes.NewAnyWithValue(&unorderedtypes.UnorderedTxExtension{TimeoutTimestamp: *timeout})
		if err != nil {
			return nil, err
		}
		extBuilder, ok := builder.(interface{ SetExtensionOptions(...*codectypes.Any) })
		if !ok {
			return nil, fmt.Errorf("tx builder does not support extension options")
		}
		extBuilder.SetExtensionOptions(ext)
	}

	addr := sdk.AccAddress(priv.PubKey().Address())
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
			ChainID:       b.chainID,
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

func (b *blaster) account(addr sdk.AccAddress) (sdk.AccountI, error) {
	res, err := b.authc.Account(context.Background(), &authtypes.QueryAccountRequest{Address: addr.String()})
	if err != nil {
		return nil, fmt.Errorf("account %s: %w", addr, err)
	}
	var acc sdk.AccountI
	if err := b.conclave.InterfaceRegistry().UnpackAny(res.Account, &acc); err != nil {
		return nil, err
	}
	return acc, nil
}

// awaitInclusion polls GetTx until every hash is in a block (or times out)
// and returns the inclusion heights.
func (b *blaster) awaitInclusion(hashes []string) ([]int64, error) {
	heights := make([]int64, len(hashes))
	deadline := time.Now().Add(30 * time.Second)
	for i, hash := range hashes {
		for {
			res, err := b.txc.GetTx(context.Background(), &txtypes.GetTxRequest{Hash: hash})
			if err == nil && res.TxResponse.Height > 0 {
				if res.TxResponse.Code != 0 {
					return nil, fmt.Errorf("tx %s failed in DeliverTx: code %d: %s",
						hash, res.TxResponse.Code, res.TxResponse.RawLog)
				}
				heights[i] = res.TxResponse.Height
				break
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("tx %s not included before deadline", hash)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	return heights, nil
}

func (b *blaster) latestHeight() (int64, error) {
	res, err := cmtservice.NewServiceClient(b.conn).GetLatestBlock(context.Background(), &cmtservice.GetLatestBlockRequest{})
	if err != nil {
		return 0, err
	}
	return res.SdkBlock.Header.Height, nil
}
