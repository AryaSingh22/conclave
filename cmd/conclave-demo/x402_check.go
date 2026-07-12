// x402-check — the live-node half of the task-2.3 surface contract
// (docs/edge/X402_SURFACE.md §7, D-083): a gateway-shaped sweep that
// exercises every node-side endpoint the X402_SURFACE.md contract names,
// once, against a running localnet. The app-level conformance suite
// (app/x402_surface_conformance_test.go) proves the auth model through
// FinalizeBlock; this subcommand proves the same surface is served by a
// real node over real gRPC. Driven by scripts/x402_surface_check.sh.
// Dev tooling only — not consensus code.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

const x402Dataset = "x402-check-dataset"

// x402Check runs the gateway-shaped live sweep. Actors: a principal
// (sigil controller), its mandate-bound gateway hot key (fresh, never a
// controller — the D-084 posture), a seller counterparty, a second
// unrelated sigil, and an unbound rogue key.
func (c *cli) x402Check() error {
	ctx := context.Background()

	// ---- actors ----
	faucet, err := c.key("faucet")
	if err != nil {
		return err
	}
	principal, err := c.ensureKey("x402-principal")
	if err != nil {
		return err
	}
	gateway, err := c.ensureKey("x402-gateway")
	if err != nil {
		return err
	}
	seller, err := c.ensureKey("x402-seller")
	if err != nil {
		return err
	}
	other, err := c.ensureKey("x402-other")
	if err != nil {
		return err
	}
	rogue, err := c.ensureKey("x402-rogue")
	if err != nil {
		return err
	}

	fund := func(to sdk.AccAddress, coins sdk.Coins) error {
		return c.deliver(faucet, &banktypes.MsgSend{
			FromAddress: addrOf(faucet).String(), ToAddress: to.String(), Amount: coins,
		})
	}
	clvOnly := sdk.NewCoins(sdk.NewInt64Coin(uclv, 100*oneCLV))
	// the PRINCIPAL (sigilP's controller) must hold the SETTLEMENT funds: a
	// coin give leg settles from the sigil's controller address
	// (conclude.go resolve→ControllerAddress), NOT the submitting gateway
	// key. In Model B (per-agent delegation) the agent owns the funds; the
	// gateway only fronts gas + bond from its own key. See X402_SURFACE.md
	// §1.2. Fund the principal with uusdc so the swap can actually settle.
	if err := fund(addrOf(principal), sdk.NewCoins(
		sdk.NewInt64Coin(uclv, 100*oneCLV), sdk.NewInt64Coin(uusdc, 1_000*usdc))); err != nil {
		return err
	}
	// the gateway float per X402_SURFACE.md §4: uusdc (fees + settlement)
	// plus a small uclv reserve (intent bonds)
	if err := fund(addrOf(gateway), sdk.NewCoins(
		sdk.NewInt64Coin(uclv, 20*oneCLV), sdk.NewInt64Coin(uusdc, 1_000*usdc))); err != nil {
		return err
	}
	if err := fund(addrOf(seller), clvOnly); err != nil {
		return err
	}
	if err := fund(addrOf(other), clvOnly); err != nil {
		return err
	}
	if err := fund(addrOf(rogue), clvOnly); err != nil {
		return err
	}

	sigilP, err := c.ensureSigil("x402-principal", principal)
	if err != nil {
		return err
	}
	sigilS, err := c.ensureSigil("x402-seller", seller)
	if err != nil {
		return err
	}
	sigilQ, err := c.ensureSigil("x402-other", other)
	if err != nil {
		return err
	}
	fmt.Println("x402-check: principals funded, sigils registered")

	// ---- bind the gateway hot key (MsgIssueMandate; §3.3 mandate shape) ----
	height, err := c.height()
	if err != nil {
		return err
	}
	ceiling := sdk.NewCoins(sdk.NewInt64Coin(uusdc, 500*usdc), sdk.NewInt64Coin(uclv, 10*oneCLV))
	if err := c.deliver(principal, &mandatetypes.MsgIssueMandate{
		Controller: addrOf(principal).String(),
		Terms: mandatetypes.MandateTerms{
			SigilId:     sigilP,
			GranteeAddr: addrOf(gateway).String(),
			AllowedMsgTypes: []string{
				sdk.MsgTypeURL(&veiltypes.MsgSubmitIntent{}),
				sdk.MsgTypeURL(&veiltypes.MsgCancelIntent{}),
				sdk.MsgTypeURL(&banktypes.MsgSend{}),
			},
			SpendCeilingPerTx:  ceiling,
			SpendCeilingWindow: ceiling,
			WindowBlocks:       86_400,
			FeeDenoms:          []string{uusdc},
			ExpiryHeight:       height + 7*86_400,
		},
	}); err != nil {
		return fmt.Errorf("issue gateway mandate: %w", err)
	}
	fmt.Println("x402-check: gateway mandate issued (submit/cancel/send; 500 USDC; fee_denoms=[uusdc]) ✓")

	// ---- query sweep: the §5 read surface, by name ----
	sba, err := c.sigilc.SigilByAddress(ctx, &sigiltypes.QuerySigilByAddressRequest{Address: addrOf(gateway).String()})
	if err != nil || sba.Sigil.Id != sigilP {
		return fmt.Errorf("sigil.SigilByAddress: gateway must resolve to %s (D-017(d)): %v", short1(sigilP), err)
	}
	mbg, err := mandatetypes.NewQueryClient(c.conn).MandateByGrantee(ctx,
		&mandatetypes.QueryMandateByGranteeRequest{Grantee: addrOf(gateway).String()})
	if err != nil || mbg.Mandate.SigilId != sigilP {
		return fmt.Errorf("mandate.MandateByGrantee: gateway headroom read failed: %v", err)
	}
	vparams, err := c.veilc.Params(ctx, &veiltypes.QueryParamsRequest{})
	if err != nil {
		return fmt.Errorf("veil.Params: %w", err)
	}
	if _, err := mandatetypes.NewQueryClient(c.conn).Params(ctx, &mandatetypes.QueryParamsRequest{}); err != nil {
		return fmt.Errorf("mandate.Params: %w", err)
	}
	if _, err := c.sessionc.Params(ctx, &sessiontypes.QueryParamsRequest{}); err != nil {
		return fmt.Errorf("session.Params: %w", err)
	}
	// on a virgin chain the typed NotFound IS the documented CurrentSession
	// response (no session has formed yet); the positive read happens after
	// the swap clears below
	if _, err := c.sessionc.CurrentSession(ctx, &sessiontypes.QueryCurrentSessionRequest{}); err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("session.CurrentSession (pre-activity): %w", err)
	}
	// Twap: cold on a fresh localnet is a valid, contract-documented response
	twap, err := c.sessionc.Twap(ctx, &sessiontypes.QueryTwapRequest{Base: uclv, Quote: uusdc})
	if err != nil {
		return fmt.Errorf("session.Twap: %w", err)
	}
	balc := banktypes.NewQueryClient(c.conn)
	if _, err := balc.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: addrOf(gateway).String(), Denom: uusdc}); err != nil {
		return fmt.Errorf("bank.Balance: %w", err)
	}
	fmt.Printf("x402-check: query sweep ✓ (min_bond=%s, twap-cold=%s)\n",
		vparams.Params.MinBond, twap.Twap)

	// ---- fee abstraction + MsgCancelIntent, on one standalone intent ----
	// the gateway submits an unmatchable intent paying the tx fee in uusdc
	// (the §4 "front gas without holding CLV" claim, asserted by the pool
	// delta), then cancels it pre-OPEN. Cancel is legal in any pre-OPEN
	// state (§5.2); with a short session cadence the OPEN edge can win the
	// race, so retry with a fresh intent — each attempt is independent.
	poolAddr := authtypes.NewModuleAddress(mandatetypes.ModuleName)
	fee := sdk.NewCoins(sdk.NewInt64Coin(uusdc, 5_000))
	feeProven := false
	cancelled := false
	for attempt := 1; attempt <= 3 && !cancelled; attempt++ {
		poolBefore, err := balc.Balance(ctx, &banktypes.QueryBalanceRequest{Address: poolAddr.String(), Denom: uusdc})
		if err != nil {
			return err
		}
		height, err = c.height()
		if err != nil {
			return err
		}
		unmatched := c.buildIntent(addrOf(gateway), sigilP,
			[]veiltypes.IntentLeg{coinLeg(uusdc, 1*usdc)},
			[]veiltypes.IntentLeg{dataLeg(fmt.Sprintf("x402-nobody-sells-%d", attempt))}, height+settleTTL)
		h, err := c.deliverFee(gateway, fee, unmatched)
		if err != nil {
			return fmt.Errorf("submit fee/cancel-target: %w", err)
		}
		id, err := c.intentIDForTx(h)
		if err != nil {
			return err
		}
		if !feeProven {
			poolAfter, err := balc.Balance(ctx, &banktypes.QueryBalanceRequest{Address: poolAddr.String(), Denom: uusdc})
			if err != nil {
				return err
			}
			if !poolAfter.Balance.Amount.Equal(poolBefore.Balance.Amount.AddRaw(5_000)) {
				return fmt.Errorf("§4 fee abstraction: pool uusdc went %s → %s, want +5000",
					poolBefore.Balance, poolAfter.Balance)
			}
			feeProven = true
			fmt.Println("x402-check: gas fronted in uusdc — fee landed in the pool (§4 fee abstraction) ✓")
		}
		code, log, err := c.deliverCode(gateway, sdk.Coins{},
			&veiltypes.MsgCancelIntent{Sender: addrOf(gateway).String(), IntentId: id})
		if err != nil {
			return fmt.Errorf("cancel broadcast: %w", err)
		}
		switch code {
		case 0:
			rec, err := c.intentRecord(id)
			if err != nil {
				return err
			}
			if rec.State != veiltypes.INTENT_STATE_CANCELLED {
				return fmt.Errorf("cancelled intent %s in state %s", short1(id), rec.State)
			}
			cancelled = true
		default:
			// lost the pre-OPEN race — the intent revealed before the cancel
			// landed; the rejection itself is the §5.2 owner/pre-OPEN gate
			fmt.Printf("x402-check: cancel attempt %d lost the pre-OPEN race (code %d: %s); retrying\n",
				attempt, code, firstLine(log))
		}
	}
	if !cancelled {
		return fmt.Errorf("MsgCancelIntent: no attempt cancelled pre-OPEN (3 tries)")
	}
	fmt.Println("x402-check: MsgCancelIntent pre-OPEN cancel + bond refund path ✓")

	// ---- the buy flow: a 2-party swap, submitted CONCURRENTLY so both
	// sides seal into ONE session (the fallback matcher pairs only within a
	// session's intent set — fallback.go). The gateway pays uusdc for a
	// metered service leg: this is the proven fallback-clearable shape
	// (coin ↔ svc, as scripts/demo_e2e.sh's fallback step uses). A
	// coin↔DATA purchase is the CONDITIONED-escrow / ZK path (2.1) or the
	// solver's cycle finder — off the surface-check path, which only needs
	// submit→seal→reveal→clear→receipt→proof exercised once. ----
	height, err = c.height()
	if err != nil {
		return err
	}
	ttl := height + settleTTL
	swap := []txSpec{
		{priv: gateway, msg: c.buildIntent(addrOf(gateway), sigilP,
			[]veiltypes.IntentLeg{coinLeg(uusdc, 100*usdc)}, []veiltypes.IntentLeg{svcLeg(svcSchema, 10)}, ttl)},
		{priv: seller, msg: c.buildIntent(addrOf(seller), sigilS,
			[]veiltypes.IntentLeg{svcLeg(svcSchema, 10)}, []veiltypes.IntentLeg{coinLeg(uusdc, 100*usdc)}, ttl)},
	}
	swapIDs, err := c.submitConcurrent(swap)
	if err != nil {
		return fmt.Errorf("submit x402 swap: %w", err)
	}
	buyID := swapIDs[0]
	// drive the session lifecycle to conclusion by advancing blocks
	// deterministically: a loaded single-node localnet does not reliably
	// tick empty blocks, and the seal→reveal→roll→auction→fallback cycle
	// needs several. Each faucet self-send waits for inclusion (deliver),
	// which forces exactly one block, so the lifecycle grinds forward
	// regardless of empty-block cadence (dev-tooling concern only).
	if err := c.driveToState(faucet, swapIDs, veiltypes.INTENT_STATE_CONCLUDED, 60); err != nil {
		for _, id := range swapIDs {
			if rec, e := c.intentRecord(id); e == nil {
				fmt.Printf("  DIAG swap intent %s: state=%s session=%d\n", short1(id), rec.State, rec.SessionId)
				if sres, e2 := c.sessionc.Session(ctx, &sessiontypes.QuerySessionRequest{Id: rec.SessionId}); e2 == nil {
					s := sres.Session
					fmt.Printf("  DIAG session %d: state=%s start=%d auctionEnd=%d intents=%d\n",
						s.Id, s.State, s.StartHeight, s.AuctionEndHeight, len(s.IntentIds))
				}
			}
		}
		return fmt.Errorf("the 2-party x402 swap did not clear: %w", err)
	}
	fmt.Println("x402-check: swap CONCLUDED (fallback matcher; no solver required) ✓")

	// session reads, now positive: the intent record names its session, the
	// record persists after settlement, and CurrentSession serves the
	// highest-id session even when closed
	buyRec, err := c.intentRecord(buyID)
	if err != nil {
		return err
	}
	sres, err := c.sessionc.Session(ctx, &sessiontypes.QuerySessionRequest{Id: buyRec.SessionId})
	if err != nil || sres.Session.Id != buyRec.SessionId {
		return fmt.Errorf("session.Session(%d): %v", buyRec.SessionId, err)
	}
	if _, err := c.sessionc.CurrentSession(ctx, &sessiontypes.QueryCurrentSessionRequest{}); err != nil {
		return fmt.Errorf("session.CurrentSession (post-activity): %w", err)
	}
	fmt.Printf("x402-check: session %d read back (Session + CurrentSession) ✓\n", buyRec.SessionId)

	// ---- settlement confirmation: Receipts → Receipt → ReceiptProof ----
	recs, err := c.receiptc.Receipts(ctx, &receipttypes.QueryReceiptsRequest{SigilId: sigilP})
	if err != nil || len(recs.Receipts) == 0 {
		return fmt.Errorf("receipt.Receipts for the gateway principal: %v (n=%d)", err, len(recs.Receipts))
	}
	rid := recs.Receipts[len(recs.Receipts)-1].Id
	if _, err := c.receiptc.Receipt(ctx, &receipttypes.QueryReceiptRequest{Id: rid}); err != nil {
		return fmt.Errorf("receipt.Receipt: %w", err)
	}
	proof, err := c.receiptc.ReceiptProof(ctx, &receipttypes.QueryReceiptProofRequest{Id: rid})
	if err != nil {
		return fmt.Errorf("receipt.ReceiptProof: %w", err)
	}
	if len(proof.Proof) == 0 || proof.ProofHeight == 0 {
		return fmt.Errorf("receipt.ReceiptProof: empty proof/height for %s", short1(rid))
	}
	fmt.Printf("x402-check: receipt %s + ICS-23 proof at height %d (X-PAYMENT-RESPONSE evidence) ✓\n",
		short1(rid), proof.ProofHeight)

	// ---- the auth chokepoints, live (§1.1) ----
	// bound key claiming a foreign sigil → ErrSigilMismatch in DeliverTx.
	// NOTE (G2, D-085): this failed tx strands its ante charge (bond-only
	// here) in the throwaway gateway mandate — see the in-tree pin test.
	wrong := c.buildIntent(addrOf(gateway), sigilQ, nil,
		[]veiltypes.IntentLeg{dataLeg(x402Dataset)}, ttl)
	code, log, err := c.deliverCode(gateway, sdk.Coins{}, wrong)
	if err != nil {
		return err
	}
	if code != veiltypes.ErrSigilMismatch.ABCICode() {
		return fmt.Errorf("bound key claiming a foreign sigil: code %d (%s), want ErrSigilMismatch", code, firstLine(log))
	}
	// unbound key claiming any sigil → ErrSigilMismatch
	rogueMsg := c.buildIntent(addrOf(rogue), sigilP, nil,
		[]veiltypes.IntentLeg{dataLeg(x402Dataset)}, ttl)
	code, log, err = c.deliverCode(rogue, sdk.Coins{}, rogueMsg)
	if err != nil {
		return err
	}
	if code != veiltypes.ErrSigilMismatch.ABCICode() {
		return fmt.Errorf("unbound key claiming a sigil: code %d (%s), want ErrSigilMismatch", code, firstLine(log))
	}
	// over-ceiling commitment → ErrMandateExceeded in the ante (CheckTx)
	over := c.buildIntent(addrOf(gateway), sigilP,
		[]veiltypes.IntentLeg{coinLeg(uusdc, 600*usdc)}, []veiltypes.IntentLeg{dataLeg(x402Dataset)}, ttl)
	ccode, _, err := c.broadcastCheck(gateway, over)
	if err != nil {
		return err
	}
	if ccode != mandatetypes.ErrMandateExceeded.ABCICode() {
		return fmt.Errorf("over-ceiling submit: CheckTx code %d, want ErrMandateExceeded", ccode)
	}
	// cross-sigil rebind of a bound grantee → ErrGranteeConflict (D-084)
	height, err = c.height()
	if err != nil {
		return err
	}
	code, log, err = c.deliverCode(other, sdk.Coins{}, &mandatetypes.MsgIssueMandate{
		Controller: addrOf(other).String(),
		Terms: mandatetypes.MandateTerms{
			SigilId:            sigilQ,
			GranteeAddr:        addrOf(gateway).String(),
			AllowedMsgTypes:    []string{sdk.MsgTypeURL(&veiltypes.MsgSubmitIntent{})},
			SpendCeilingPerTx:  ceiling,
			SpendCeilingWindow: ceiling,
			WindowBlocks:       86_400,
			ExpiryHeight:       height + 86_400,
		},
	})
	if err != nil {
		return err
	}
	if code != mandatetypes.ErrGranteeConflict.ABCICode() {
		return fmt.Errorf("cross-sigil rebind: code %d (%s), want ErrGranteeConflict (D-084)", code, firstLine(log))
	}
	fmt.Println("x402-check: auth chokepoints live — foreign-sigil, unbound-key, over-ceiling, cross-sigil rebind all rejected ✓")

	fmt.Println("X402 SURFACE CHECK OK")
	return nil
}

// deliverFee is deliver() with an explicit fee (the §4 fee-abstraction
// path needs a real uusdc fee on the wire); returns the tx hash.
func (c *cli) deliverFee(priv cryptotypes.PrivKey, fee sdk.Coins, msgs ...sdk.Msg) (string, error) {
	hash, code, log, err := c.broadcastAndWait(priv, fee, msgs...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("tx %s failed: code %d: %s", hash, code, firstLine(log))
	}
	return hash, nil
}

// deliverCode is deliver() with an explicit fee that returns the DeliverTx
// code instead of failing — for the live negative cases, whose rejections
// happen in the msg server (past CheckTx).
func (c *cli) deliverCode(priv cryptotypes.PrivKey, fee sdk.Coins, msgs ...sdk.Msg) (uint32, string, error) {
	_, code, log, err := c.broadcastAndWait(priv, fee, msgs...)
	return code, log, err
}

func (c *cli) broadcastAndWait(priv cryptotypes.PrivKey, fee sdk.Coins, msgs ...sdk.Msg) (string, uint32, string, error) {
	acc, err := c.account(addrOf(priv))
	if err != nil {
		return "", 0, "", err
	}
	bz, err := c.signTxFee(priv, acc.GetAccountNumber(), acc.GetSequence(), fee, msgs...)
	if err != nil {
		return "", 0, "", err
	}
	res, err := c.txc.BroadcastTx(context.Background(), &txtypes.BroadcastTxRequest{
		TxBytes: bz, Mode: txtypes.BroadcastMode_BROADCAST_MODE_SYNC,
	})
	if err != nil {
		return "", 0, "", err
	}
	sum := sha256.Sum256(bz)
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	if res.TxResponse.Code != 0 {
		return hash, res.TxResponse.Code, res.TxResponse.RawLog, nil
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		got, err := c.txc.GetTx(context.Background(), &txtypes.GetTxRequest{Hash: hash})
		if err == nil && got.TxResponse.Height > 0 {
			return hash, got.TxResponse.Code, got.TxResponse.RawLog, nil
		}
		if time.Now().After(deadline) {
			return hash, 0, "", fmt.Errorf("tx %s not included before deadline", hash)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// driveToState advances the chain one block at a time (each faucet self-send
// waits for inclusion, forcing a block) until every id reaches want or
// maxBlocks is exhausted. It makes the session lifecycle deterministic on a
// single-node localnet whose empty-block cadence is unreliable under the
// demo client's load — the seal→reveal→roll→auction→fallback cycle needs
// several blocks, and querying alone never produces them.
func (c *cli) driveToState(priv cryptotypes.PrivKey, ids []string, want veiltypes.IntentState, maxBlocks int) error {
	addr := addrOf(priv)
	for b := 0; b < maxBlocks; b++ {
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
		// one no-op self-send, waited to inclusion → exactly one new block
		if err := c.deliver(priv, &banktypes.MsgSend{
			FromAddress: addr.String(), ToAddress: addr.String(),
			Amount: sdk.NewCoins(sdk.NewInt64Coin(uclv, 1)),
		}); err != nil {
			return fmt.Errorf("block-nudge send: %w", err)
		}
	}
	return fmt.Errorf("intents did not all reach %s within %d blocks", want, maxBlocks)
}
