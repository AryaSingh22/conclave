// exit-demo — the live driver for the Phase-2 §6 exit demo, part A (§6a).
//
// WHAT §6a ASSERTS. Three of the four exit legs, running TOGETHER on a real
// node for the first time:
//
//	x402 ingress (2.3)   a Model-A gateway principal submits a mandate-bound
//	                     buy with gas fronted in the settlement denom through
//	                     the §6.2 fee abstraction
//	ZK escrow (2.1b)     the buy is a CONDITIONED ZK_DELIVERY purchase: both
//	                     parties sign the same ZkDeliveryCondition, CONCLUDE
//	                     routes the payment into a funded ZK escrow
//	CVM release (2.2)    a REAL CosmWasm contract (demo/cvm-contract), uploaded
//	                     through governance, submits the groth16 proof and
//	                     releases the escrow — D-079's deferred localnet
//	                     contract-release smoke, which had no vehicle until
//	                     that contract existed
//
// The fourth leg (IBC USDC + receipt verification against a second chain's
// light client) is §6b's — see exit_demo_ibc.go. §6a settles in literal uusdc;
// §6b re-runs THIS SAME FLOW on the ibc/<hash> voucher by setting
// exitState.settleDenom, so the stages below are denom-agnostic and
// scripts/exit_demo.sh doubles as the regression test for that parametrization.
//
// THE CONDITION-ATTACHING PROPOSER — READ THIS BEFORE REUSING IT.
//
// CURRENT STATE (task 2.7, D-094). A signed delivery condition is a
// REQUIREMENT, not a permission ceiling. `attached => signed` AND
// `signed => attached` are both enforced, at both chokepoints
// (x/session/keeper/condition_binding.go), and the in-consensus fallback
// ATTACHES the giver's signed condition rather than dropping it
// (attachFallbackCondition, x/session/keeper/fallback.go) — so the network
// HAS a live writer of SettleLeg.Condition, and it is consensus itself.
// A condition may also be carried per-leg on IntentLeg.condition (FW-2), which
// REFINES the payload-level IntentPayload.condition; effectiveCondition
// resolves which one governs a leg for a party. Dropping a signed condition is
// ErrConditionNotHonored (session 1429).
//
// The buyer's relation is per-class, and a reference attacher must honour it:
// seller and ZK buyer EXACT; HASHLOCK buyer attached timeout <= signed (a
// MAXIMUM — the timer is when the buyer's locked payment refunds); OPTIMISTIC
// buyer attached window >= signed (a MINIMUM — the window IS the buyer's
// inspect-and-dispute protection).
//
// CORRECTED at task 2.17. This block used to read:
//
//	"STILL TRUE, and it is task 2.14's: the reference solver in solver/ never
//	reads IntentPayload.condition, so it would propose a SLASHABLE (5%)
//	solution the first time it meets a condition-signing intent."
//
// Both halves are now wrong. Task 2.14 (D-097) made solver/ attach — see
// solver/condition and docs/edge/SOLVER_SURFACE.md §5 — and 2.14's OWN
// negation sweep corrected the sibling statement in exit_demo_ibc.go while
// missing this one, which is why 2.17's sweep looked at comments and not only
// at printed banner strings. And the "SLASHABLE (5%)" price did not exist when
// it was written: the slash was reverted with the failing tx until task 2.17
// moved it into the ante (D-097 finding F-1). It exists now.
//
// SUPERSEDED — kept for the record (D-091/D-093, both closed by D-094; the
// forward pointer is condition_binding.go and D-094, above):
//
//	"A delivery condition is SOLVER-ATTACHED by design (D-075: IntentLeg has no
//	condition field). Neither the in-consensus fallback matcher nor the
//	reference solver ever attaches one, and escrows are created ONLY by
//	CONCLUDE, so before this file NO component in the repo could produce a
//	conditioned escrow on a live network. VERDICT (2.13, D-093): the consent
//	gates run in ONE direction — `signed => attached` is enforced at neither
//	chokepoint — so a signed condition is a PERMISSION CEILING, never a
//	requirement, and the fallback downgrades every condition-signing pair it
//	clears. Pinned by TestSignedConditionIsPermissionNotRequirement_BoundaryPin."
//
// That pin was REPLACED, not relaxed, by
// TestSignedConditionIsRequirement_Enforced (x/session/keeper) — D-094.
// proposeConditionedSolution below remains a valid attacher and was the FIRST
// live-network writer; it is no longer the only one.
//
// This proposer lives here and NOT in solver/ deliberately: solver/ is imported
// by x/session/simulation, so changing it puts the determinism vector on the
// critical path (METHODOLOGY Rule 7) — the constraint task 2.14 opens its
// plan-stop on.
//
// Dev tooling only — not consensus code.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/CosmWasm/wasmd/x/wasm/ioutils"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"

	"cosmossdk.io/math"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/AryaSingh22/conclave/crypto/zkdelivery"
	"github.com/AryaSingh22/conclave/crypto/zkdelivery/zktest"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

const (
	exitDataset = "conclave-exit-demo-dataset-ZK"
	// the "data" the seller delivers; its MiMC commitment is what both parties
	// sign, and knowing it is what lets the prover produce a releasing proof
	exitPayloadText = "conclave exit demo: ZK-verified dataset payload v1"
	exitPrice       = 250 * usdc // uusdc the buyer pays for the dataset
	exitFee         = 5_000      // uusdc tx fee, converted through the §6.2 pool
	// the store-code proposal carries the whole (gzipped) contract, so its gas
	// is dominated by tx size rather than execution
	storeCodeGas = 60_000_000
)

// exitState carries the actors and artifacts across the demo's stages.
type exitState struct {
	faucet, validator          cryptotypes.PrivKey
	principal, gateway, seller cryptotypes.PrivKey
	solver, deployer           cryptotypes.PrivKey
	sigilP, sigilS             string
	payload, schema            []byte
	cond                       *escrowtypes.DeliveryCondition
	buyID, sellID              string
	sessionID                  uint64
	escrowID                   string
	codeID                     uint64
	contract                   string

	// settleDenom is the asset the purchase settles in — the mandate's
	// fee_denoms and ceilings, the buy's give leg, the escrow's held amount,
	// and the clearing-price numeraire all read it. §6a settles in the chain's
	// native "uusdc"; §6b sets it to the ibc/<hash> VOUCHER received over a
	// live relayer, which is the whole point of that half (D-082). Every stage
	// below is denom-agnostic so the two runs share ONE code path: §6a's
	// scripts/exit_demo.sh is therefore also the regression test that §6b's
	// parametrization did not change §6a's behavior.
	settleDenom string
	// ibc carries §6b's cross-chain artifacts; nil for a §6a (native) run.
	ibc *ibcLeg
}

func (c *cli) exitDemo() error {
	fmt.Println("=== Conclave Phase-2 §6 EXIT DEMO — part A (§6a) ===")
	fmt.Println("=== INTERNAL ACCEPTANCE RUN (D-090): the settlement is CONDITIONED, so its")
	fmt.Println("=== receipt reads condition_outcome=NONE (D-087). Task 2.9 is a PREREQUISITE")
	fmt.Println("=== if this output is ever repurposed as external-facing evidence.")
	fmt.Println()

	s := &exitState{settleDenom: uusdc}
	for _, step := range []struct {
		name string
		run  func(*exitState) error
	}{
		{"stage 1 — actors, sigils, gateway mandate", c.exitStageActors},
		{"stage 1b — the gateway mandate", c.exitStageMandate},
		{"stage 2 — x402 ingress: the conditioned ZK buy", c.exitStageBuy},
		{"stage 3 — conditioned settlement: the ZK escrow opens", c.exitStageSettle},
		{"stage 4 — the CVM contract, uploaded through governance", c.exitStageUpload},
		{"stage 5 — the contract releases the escrow (D-079)", c.exitStageRelease},
		{"stage 6 — evidence: what the receipt proves, and what it does NOT", c.exitStageEvidence},
		{"stage 7 — the consent gate, reached from the live path", c.exitStageConsentNegative},
	} {
		fmt.Printf("---- %s ----\n", step.name)
		if err := step.run(s); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
		fmt.Println()
	}

	fmt.Println("==================================================")
	fmt.Println(" EXIT DEMO §6a PASSED — x402 ingress + ZK escrow + CVM release, live")
	fmt.Println(" (§6b adds IBC USDC ingress and second-chain receipt verification)")
	fmt.Println("==================================================")
	return nil
}

// ---- stage 1: actors -------------------------------------------------------

func (c *cli) exitStageActors(s *exitState) error {
	var err error
	if s.faucet, err = c.key("faucet"); err != nil {
		return err
	}
	if s.validator, err = c.key("validator"); err != nil {
		return err
	}
	// solver1 is pre-registered and bonded in the localnet genesis (SPEC §12),
	// so the demo proposes from an already-ACTIVE solver.
	if s.solver, err = c.key("solver1"); err != nil {
		return err
	}
	for name, dst := range map[string]*cryptotypes.PrivKey{
		"exit-principal": &s.principal,
		"exit-gateway":   &s.gateway,
		"exit-seller":    &s.seller,
		"exit-deployer":  &s.deployer,
	} {
		k, err := c.ensureKey(name)
		if err != nil {
			return err
		}
		*dst = k
	}

	fund := func(to sdk.AccAddress, coins sdk.Coins) error {
		return c.deliver(s.faucet, &banktypes.MsgSend{
			FromAddress: addrOf(s.faucet).String(), ToAddress: to.String(), Amount: coins,
		})
	}
	// The PRINCIPAL holds the settlement funds: a coin give leg settles from
	// the sigil's CONTROLLER address, not from the submitting gateway key
	// (X402_SURFACE §1.2 custody split). The gateway key fronts only gas+bond.
	//
	// Native (§6a) runs take the settlement asset from the faucet. In the §6b
	// IBC run the settlement asset does not EXIST on this chain yet — it
	// arrives as an ibc/<hash> voucher over the live relayer in stage 1a — so
	// only the CLV gas float is funded here.
	principalFunds := sdk.NewCoins(sdk.NewInt64Coin(uclv, 100*oneCLV))
	gatewayFunds := sdk.NewCoins(sdk.NewInt64Coin(uclv, 50*oneCLV))
	if s.ibc == nil {
		principalFunds = principalFunds.Add(sdk.NewInt64Coin(uusdc, 2_000*usdc))
		gatewayFunds = gatewayFunds.Add(sdk.NewInt64Coin(uusdc, 500*usdc))
	}
	if err := fund(addrOf(s.principal), principalFunds); err != nil {
		return err
	}
	if err := fund(addrOf(s.gateway), gatewayFunds); err != nil {
		return err
	}
	if err := fund(addrOf(s.seller), sdk.NewCoins(sdk.NewInt64Coin(uclv, 50*oneCLV))); err != nil {
		return err
	}
	if err := fund(addrOf(s.deployer), sdk.NewCoins(sdk.NewInt64Coin(uclv, 20_000*oneCLV))); err != nil {
		return err
	}

	if s.sigilP, err = c.ensureSigil("exit-principal", s.principal); err != nil {
		return err
	}
	if s.sigilS, err = c.ensureSigil("exit-seller", s.seller); err != nil {
		return err
	}

	fmt.Printf("actors: principal=%s gateway(hot)=%s seller=%s\n",
		short1(s.sigilP), addrOf(s.gateway), short1(s.sigilS))
	return nil
}

// exitStageMandate issues the gateway's mandate. SPLIT OUT of exitStageActors
// for §6b: the mandate's ceilings and fee_denoms are denominated in the
// SETTLEMENT asset, and in the IBC run that asset's denom is not known until
// the voucher has actually landed (stage 1a resolves it). Same code, both runs.
func (c *cli) exitStageMandate(s *exitState) error {
	if s.settleDenom == "" {
		return fmt.Errorf("settlement denom unresolved before the mandate is issued")
	}
	height, err := c.height()
	if err != nil {
		return err
	}
	// Model A (X402_SURFACE §1.2): the gateway operator's own sigil, its hot key
	// mandate-bound. fee_denoms carries the settlement denom so gas is fronted
	// in it through §6.2 abstraction — in §6b that is the ibc/<hash> voucher,
	// i.e. the ceilings BIND in an IBC asset (the 2.5/D-082 property, live).
	ceiling := sdk.NewCoins(sdk.NewInt64Coin(s.settleDenom, 1_000*usdc), sdk.NewInt64Coin(uclv, 10*oneCLV))
	if err := c.deliver(s.principal, &mandatetypes.MsgIssueMandate{
		Controller: addrOf(s.principal).String(),
		Terms: mandatetypes.MandateTerms{
			SigilId:     s.sigilP,
			GranteeAddr: addrOf(s.gateway).String(),
			AllowedMsgTypes: []string{
				sdk.MsgTypeURL(&veiltypes.MsgSubmitIntent{}),
				sdk.MsgTypeURL(&veiltypes.MsgCancelIntent{}),
			},
			SpendCeilingPerTx:  ceiling,
			SpendCeilingWindow: ceiling,
			WindowBlocks:       86_400,
			FeeDenoms:          []string{s.settleDenom},
			ExpiryHeight:       height + 7*86_400,
		},
	}); err != nil {
		return fmt.Errorf("issue gateway mandate: %w", err)
	}
	fmt.Printf("gateway mandate issued (submit/cancel only; fee_denoms=[%s]) ✓\n", shortDenom(s.settleDenom))
	return nil
}

// ---- stage 2: the conditioned ZK buy ---------------------------------------

func (c *cli) exitStageBuy(s *exitState) error {
	// The seller's dataset and the commitment BOTH parties sign. The buyer can
	// verify the commitment against the dataset it was quoted; the seller can
	// prove knowledge of the payload without revealing it on chain.
	s.payload = []byte(exitPayloadText)
	s.schema = zktest.SchemaCommitment(s.payload)
	s.cond = &escrowtypes.DeliveryCondition{
		Condition: &escrowtypes.DeliveryCondition_ZkDelivery{
			ZkDelivery: &escrowtypes.ZkDeliveryCondition{
				SchemaCommitment: s.schema,
				VkId:             zkdelivery.VKIDDeliveryV1,
			},
		},
	}

	height, err := c.height()
	if err != nil {
		return err
	}
	ttl := height + settleTTL

	balc := banktypes.NewQueryClient(c.conn)
	poolAddr := authtypes.NewModuleAddress(mandatetypes.ModuleName)
	poolBefore, err := balc.Balance(context.Background(), &banktypes.QueryBalanceRequest{
		Address: poolAddr.String(), Denom: s.settleDenom})
	if err != nil {
		return err
	}

	// Both intents must seal into ONE session to be matchable, so they are
	// submitted concurrently. The BUY carries a uusdc fee (the x402 leg's
	// gas-fronting claim); the SELL pays in uclv like any native agent.
	// allow_partial is deliberately LEFT UNSET on both: a purchase must be
	// fill-or-kill (X402_SURFACE §3.4(b)), and a DataGrant is exact-fill anyway.
	buy := c.buildIntentCond(addrOf(s.gateway), s.sigilP,
		[]veiltypes.IntentLeg{coinLeg(s.settleDenom, exitPrice)},
		[]veiltypes.IntentLeg{dataLeg(exitDataset)}, ttl, s.cond)
	sell := c.buildIntentCond(addrOf(s.seller), s.sigilS,
		[]veiltypes.IntentLeg{dataLeg(exitDataset)},
		[]veiltypes.IntentLeg{coinLeg(s.settleDenom, exitPrice)}, ttl, s.cond)

	ids, err := c.submitConcurrent([]txSpec{
		{priv: s.gateway, msg: buy, fee: sdk.NewCoins(sdk.NewInt64Coin(s.settleDenom, exitFee))},
		{priv: s.seller, msg: sell},
	})
	if err != nil {
		return fmt.Errorf("submit the conditioned pair: %w", err)
	}
	s.buyID, s.sellID = ids[0], ids[1]

	poolAfter, err := balc.Balance(context.Background(), &banktypes.QueryBalanceRequest{
		Address: poolAddr.String(), Denom: s.settleDenom})
	if err != nil {
		return err
	}
	if !poolAfter.Balance.Amount.Equal(poolBefore.Balance.Amount.AddRaw(exitFee)) {
		return fmt.Errorf("x402 leg: gas was not fronted in %s — pool %s → %s, want +%d",
			shortDenom(s.settleDenom), poolBefore.Balance, poolAfter.Balance, exitFee)
	}
	fmt.Printf("x402 ingress: buy %s submitted by the mandate-bound hot key, gas fronted in %s (§6.2 abstraction, pool +%d) ✓\n",
		short1(s.buyID), shortDenom(s.settleDenom), exitFee)
	fmt.Printf("counterparty: seller intent %s (same ZK condition signed by BOTH parties — D-076) ✓\n", short1(s.sellID))
	return nil
}

// ---- stage 3: the conditioned settlement -----------------------------------

func (c *cli) exitStageSettle(s *exitState) error {
	ctx := context.Background()
	if err := c.waitSameSession([]string{s.buyID, s.sellID}); err != nil {
		return err
	}
	rec, err := c.intentRecord(s.buyID)
	if err != nil {
		return err
	}
	s.sessionID = rec.SessionId
	fmt.Printf("both intents OPEN in session %d\n", s.sessionID)

	if err := c.proposeConditionedSolution(s); err != nil {
		return err
	}
	if err := c.driveToState(s.faucet, []string{s.buyID, s.sellID}, veiltypes.INTENT_STATE_CONCLUDED, 40); err != nil {
		return fmt.Errorf("the conditioned pair did not settle: %w", err)
	}

	// the settlement must have opened a FUNDED ZK escrow, not paid the seller
	escrowc := escrowtypes.NewQueryClient(c.conn)
	res, err := escrowc.Escrows(ctx, &escrowtypes.QueryEscrowsRequest{SigilId: s.sigilP})
	if err != nil {
		return fmt.Errorf("escrow.Escrows: %w", err)
	}
	var esc *escrowtypes.Escrow
	for i := range res.Escrows {
		if res.Escrows[i].SessionId == s.sessionID {
			esc = &res.Escrows[i]
		}
	}
	if esc == nil {
		return fmt.Errorf("no escrow opened for session %d — the settlement cleared UNCONDITIONED "+
			"(the fallback matcher won the auction; widen SOLUTION_WINDOW_BLOCKS)", s.sessionID)
	}
	s.escrowID = esc.Id
	if esc.State != escrowtypes.ESCROW_STATE_ACTIVE {
		return fmt.Errorf("escrow %s state %s, want ACTIVE", short1(esc.Id), esc.State)
	}
	if esc.Condition.GetZkDelivery() == nil {
		return fmt.Errorf("escrow %s is not ZK_DELIVERY", short1(esc.Id))
	}
	if esc.PayerSigil != s.sigilP || esc.PayeeSigil != s.sigilS {
		return fmt.Errorf("escrow parties wrong: payer=%s payee=%s", short1(esc.PayerSigil), short1(esc.PayeeSigil))
	}
	want := sdk.NewCoins(sdk.NewInt64Coin(s.settleDenom, exitPrice))
	if !esc.Amount.Equal(want) {
		return fmt.Errorf("escrow holds %s, want %s", esc.Amount, want)
	}
	// the payment is HELD, not delivered: the seller has not been paid yet
	balc := banktypes.NewQueryClient(c.conn)
	sellerBal, err := balc.Balance(context.Background(), &banktypes.QueryBalanceRequest{
		Address: addrOf(s.seller).String(), Denom: s.settleDenom})
	if err != nil {
		return err
	}
	if !sellerBal.Balance.Amount.IsZero() {
		return fmt.Errorf("seller already holds %s before release — payment was not escrowed", sellerBal.Balance)
	}
	fmt.Printf("ZK escrow %s ACTIVE, holding %s; seller NOT paid until a proof lands ✓\n",
		short1(s.escrowID), esc.Amount)
	return nil
}

// proposeConditionedSolution waits for the session to reach AUCTION and submits
// the bilateral match with the signed condition attached to the DataGrant leg.
// This is the first live-network writer of SettleLeg.Condition (see the package
// comment); the chain then re-derives consent independently — validateSolution
// checks the attached condition against BOTH parties' signed payloads
// (D-075 seller, D-076 buyer) and rejects the solution if they differ.
func (c *cli) proposeConditionedSolution(s *exitState) error {
	if err := c.waitForAuction(s.sessionID); err != nil {
		return err
	}
	code, _, log, err := c.proposeMatch(s.solver, s.sessionID,
		conditionedMatch(s.buyID, s.sellID, s.sigilP, s.sigilS, s.cond, s.settleDenom))
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("propose conditioned solution: code %d: %s", code, firstLine(log))
	}
	fmt.Println("conditioned solution proposed by solver1 (condition attached to the DataGrant leg) ✓")
	return nil
}

// conditionedMatch is the bilateral data-for-coin settlement graph with the
// condition attached to the DataGrant leg (D-074's two-party shape).
func conditionedMatch(buyID, sellID, buyerSigil, sellerSigil string,
	cond *escrowtypes.DeliveryCondition, denom string) sessiontypes.Match {
	dataHash := sha256.Sum256([]byte(exitDataset))
	payment := sdk.NewInt64Coin(denom, exitPrice)
	return sessiontypes.Match{
		IntentIds: []string{buyID, sellID},
		Legs: []sessiontypes.SettleLeg{
			{
				FromSigil: sellerSigil, ToSigil: buyerSigil,
				Asset:     &sessiontypes.SettleLeg_Data{Data: &veiltypes.DataGrant{ContentHash: dataHash[:]}},
				Condition: cond,
			},
			{
				FromSigil: buyerSigil, ToSigil: sellerSigil,
				Asset: &sessiontypes.SettleLeg_Coin{Coin: &payment},
			},
		},
		// prices in the SETTLEMENT numeraire (§6b: the ibc/<hash> voucher): the
		// dataset clears at the payment, the numeraire at 1. CheckPriceBinding
		// demands an EXACT zero residual per party, so these are the only prices
		// this match can carry.
		ClearingPrices: []sessiontypes.PricePoint{
			{Base: denom, Quote: denom, Price: math.LegacyOneDec()},
			{Base: "data/" + hex.EncodeToString(dataHash[:]), Quote: denom, Price: math.LegacyNewDec(exitPrice)},
		},
	}
}

// proposeMatch returns the tx result's (code, codespace, log). The codespace is
// carried because task 2.13's stage-7 assertion dispatches on the PAIR, which is
// what a real off-chain solver must do (D-093).
func (c *cli) proposeMatch(solver cryptotypes.PrivKey, sessionID uint64, m sessiontypes.Match) (uint32, string, string, error) {
	_, code, space, log, err := c.broadcastAndWaitFull(solver, sdk.Coins{}, &sessiontypes.MsgProposeSolution{
		Solver:    addrOf(solver).String(),
		SessionId: sessionID,
		Matches:   []sessiontypes.Match{m},
	})
	return code, space, log, err
}

func (c *cli) waitForAuction(sessionID uint64) error {
	ctx := context.Background()
	deadline := time.Now().Add(waitTimeout)
	for {
		res, err := c.sessionc.Session(ctx, &sessiontypes.QuerySessionRequest{Id: sessionID})
		if err == nil {
			switch res.Session.State {
			case sessiontypes.SESSION_STATE_AUCTION:
				return nil
			case sessiontypes.SESSION_STATE_SCORING, sessiontypes.SESSION_STATE_CONCLUDING,
				sessiontypes.SESSION_STATE_FALLBACK, sessiontypes.SESSION_STATE_CLOSED:
				return fmt.Errorf("session %d left AUCTION (state %s) before a solution was proposed — "+
					"the auction window is too short for this driver; widen SOLUTION_WINDOW_BLOCKS",
					sessionID, res.Session.State)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("session %d never reached AUCTION", sessionID)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// ---- stage 4: the CVM contract, via governance -----------------------------

func (c *cli) exitStageUpload(s *exitState) error {
	ctx := context.Background()
	wasmPath := os.Getenv("CONCLAVE_DEMO_CONTRACT")
	if wasmPath == "" {
		wasmPath = filepath.Join("demo", "cvm-contract", "artifacts", "conclave_demo_escrow.wasm")
	}
	raw, err := os.ReadFile(wasmPath)
	if err != nil {
		return fmt.Errorf("read demo contract (build it with `make demo-contract`): %w", err)
	}
	// wasmd accepts a gzipped payload and gunzips it before validation; at ~212 KB
	// raw the store tx's SIZE cost alone dominates its gas.
	code, err := ioutils.GzipIt(raw)
	if err != nil {
		return fmt.Errorf("gzip contract: %w", err)
	}

	// Upload is gov-only: the default genesis ships CodeUploadAccess=Nobody and
	// InstantiateDefaultPermission=Nobody (D-079). Prove it, then use it.
	deployer := addrOf(s.deployer).String()
	storeMsg := &wasmtypes.MsgStoreCode{
		Sender:       deployer,
		WASMByteCode: code,
		InstantiatePermission: &wasmtypes.AccessConfig{
			Permission: wasmtypes.AccessTypeAnyOfAddresses,
			Addresses:  []string{deployer},
		},
	}
	// Assert the SPECIFIC rejection, not merely a non-zero code: the first run of
	// this demo "passed" this check on code 11 (out of gas on tx size), which
	// proves nothing about the upload permission. Requiring ErrUnauthorized is
	// what makes it a posture proof (METHODOLOGY Rule 4).
	rejCode, rejLog, err := c.deliverGas(s.deployer, storeCodeGas, storeMsg)
	if err != nil {
		return err
	}
	if rejCode != sdkerrors.ErrUnauthorized.ABCICode() {
		return fmt.Errorf("permissioned upload posture: non-gov MsgStoreCode got code %d (%s), want ErrUnauthorized (D-079)",
			rejCode, firstLine(rejLog))
	}
	fmt.Printf("permissioned posture live: direct (non-gov) MsgStoreCode → ErrUnauthorized (code %d) ✓\n", rejCode)

	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	storeMsg.Sender = govAddr
	govParams, err := govv1.NewQueryClient(c.conn).Params(ctx, &govv1.QueryParamsRequest{ParamsType: "deposit"})
	if err != nil {
		return fmt.Errorf("gov.Params: %w", err)
	}
	prop, err := govv1.NewMsgSubmitProposal(
		[]sdk.Msg{storeMsg}, govParams.Params.MinDeposit, deployer,
		"", "exit demo: store the CVM demo contract", "Phase-2 §6 exit demo (D-079 contract-release smoke)", false)
	if err != nil {
		return err
	}
	propCode, propLog, err := c.deliverGas(s.deployer, storeCodeGas, prop)
	if err != nil {
		return fmt.Errorf("submit store-code proposal: %w", err)
	}
	if propCode != 0 {
		return fmt.Errorf("submit store-code proposal: code %d: %s", propCode, firstLine(propLog))
	}
	props, err := govv1.NewQueryClient(c.conn).Proposals(ctx, &govv1.QueryProposalsRequest{
		ProposalStatus: govv1.StatusVotingPeriod})
	if err != nil || len(props.Proposals) == 0 {
		return fmt.Errorf("no proposal in voting period: %v", err)
	}
	propID := props.Proposals[len(props.Proposals)-1].Id
	if err := c.deliver(s.validator, &govv1.MsgVote{
		ProposalId: propID, Voter: addrOf(s.validator).String(), Option: govv1.OptionYes,
	}); err != nil {
		return fmt.Errorf("vote on proposal %d: %w", propID, err)
	}
	fmt.Printf("store-code proposal %d submitted and voted YES; waiting out the voting period\n", propID)

	deadline := time.Now().Add(3 * time.Minute)
	for {
		res, err := govv1.NewQueryClient(c.conn).Proposal(ctx, &govv1.QueryProposalRequest{ProposalId: propID})
		if err == nil {
			switch res.Proposal.Status {
			case govv1.StatusPassed:
				goto passed
			case govv1.StatusRejected, govv1.StatusFailed:
				// surface the execution error here rather than leaving it in the
				// node log: this is exactly how the reference-types toolchain
				// incompatibility was found (D-091)
				return fmt.Errorf("store-code proposal %d ended %s: %s",
					propID, res.Proposal.Status, res.Proposal.FailedReason)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("store-code proposal %d never resolved", propID)
		}
		time.Sleep(time.Second)
	}
passed:
	codesRes, err := wasmtypes.NewQueryClient(c.conn).Codes(ctx, &wasmtypes.QueryCodesRequest{})
	if err != nil || len(codesRes.CodeInfos) == 0 {
		return fmt.Errorf("no code stored after the proposal passed: %v", err)
	}
	s.codeID = codesRes.CodeInfos[len(codesRes.CodeInfos)-1].CodeID
	fmt.Printf("contract stored through governance as code_id=%d ✓\n", s.codeID)

	if err := c.deliver(s.deployer, &wasmtypes.MsgInstantiateContract{
		Sender: deployer, Admin: deployer, CodeID: s.codeID,
		Label: "conclave-exit-demo-escrow", Msg: []byte(`{}`),
	}); err != nil {
		return fmt.Errorf("instantiate: %w", err)
	}
	byCode, err := wasmtypes.NewQueryClient(c.conn).ContractsByCode(ctx,
		&wasmtypes.QueryContractsByCodeRequest{CodeId: s.codeID})
	if err != nil || len(byCode.Contracts) == 0 {
		return fmt.Errorf("no contract instantiated for code %d: %v", s.codeID, err)
	}
	s.contract = byCode.Contracts[len(byCode.Contracts)-1]
	fmt.Printf("contract instantiated at %s ✓\n", s.contract)
	return nil
}

// ---- stage 5: the contract releases the escrow -----------------------------

func (c *cli) exitStageRelease(s *exitState) error {
	escrowc := escrowtypes.NewQueryClient(c.conn)
	idBz, err := escrowtypes.IDBytes(s.escrowID)
	if err != nil {
		return err
	}

	// (a) NEGATIVE, before the positive: a proof generated for a DIFFERENT
	// escrow instance must not release this one. The verifier rebuilds the
	// public witness from the RECORD, so the binding is instance-specific
	// (D-076) — and a contract cannot influence it either.
	otherID := sha256.Sum256([]byte("some other escrow instance"))
	wrongProof, err := zktest.Prove(otherID[:], s.schema, s.payload)
	if err != nil {
		return fmt.Errorf("generate wrong-instance proof: %w", err)
	}
	code, log, err := c.execContract(s, map[string]any{
		"submit_delivery_proof": map[string]any{"escrow_id": s.escrowID, "proof": wrongProof},
	})
	if err != nil {
		return err
	}
	if code == 0 {
		return fmt.Errorf("D-076 instance binding BROKEN: a proof for another escrow released this one")
	}
	// Assert the SPECIFIC rejection. §6a shipped this negative checking only
	// `code != 0`, which is the very shape §6a's own finding F-3 was about: a
	// wrong-instance proof rejected for ANY other reason (wrong class, terminal
	// state, out of gas) would have "passed" it while proving nothing about
	// instance binding — and this negative is the live proof of D-076. Found on
	// re-reading it in §6b; tightened here (D-092 finding F-1).
	if code != escrowtypes.ErrInvalidProof.ABCICode() {
		return fmt.Errorf("wrong-instance proof: code %d (%s), want ErrInvalidProof — a rejection for any "+
			"other reason proves nothing about D-076 instance binding", code, firstLine(log))
	}
	fmt.Printf("instance binding holds: a proof bound to another escrow is rejected ErrInvalidProof (code %d: %s) ✓\n",
		code, firstLine(log))

	// (b) NEGATIVE: the contract tries the preimage path on a ZK escrow. The
	// allowlist encodes it, and the KEEPER rejects it as wrong-class — proving
	// the binding reaches the real gate rather than a demo-side check.
	code, log, err = c.execContract(s, map[string]any{
		"release_escrow": map[string]any{"escrow_id": s.escrowID, "preimage": []byte("not-a-proof")},
	})
	if err != nil {
		return err
	}
	if code != escrowtypes.ErrWrongClass.ABCICode() {
		return fmt.Errorf("preimage release on a ZK escrow: code %d (%s), want ErrWrongClass", code, firstLine(log))
	}
	fmt.Println("class gate holds: contract-driven preimage release on a ZK escrow → ErrWrongClass ✓")

	// (c) NEGATIVE: a real contract attempting settlement authorship. The CVM
	// allowlist has no `conclude` branch, so the encoder rejects it (layer 1,
	// D-079) — watched here from a contract that genuinely emitted it.
	code, log, err = c.execContract(s, map[string]any{
		"forge_conclude": map[string]any{"session_id": s.sessionID},
	})
	if err != nil {
		return err
	}
	if code != sdkerrors.ErrUnauthorized.ABCICode() {
		return fmt.Errorf("contract-emitted conclude: code %d (%s), want ErrUnauthorized", code, firstLine(log))
	}
	fmt.Println("allowlist holds: a real contract emitting `conclude` is rejected by the CVM encoder ✓")

	// (d) POSITIVE — D-079's deferred smoke: the contract submits a proof bound
	// to THIS escrow and the escrow releases to the seller.
	proof, err := zktest.Prove(idBz, s.schema, s.payload)
	if err != nil {
		return fmt.Errorf("generate delivery proof: %w", err)
	}
	code, log, err = c.execContract(s, map[string]any{
		"submit_delivery_proof": map[string]any{"escrow_id": s.escrowID, "proof": proof},
	})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("contract release failed: code %d: %s", code, firstLine(log))
	}
	res, err := escrowc.Escrow(context.Background(), &escrowtypes.QueryEscrowRequest{Id: s.escrowID})
	if err != nil {
		return err
	}
	if res.Escrow.State != escrowtypes.ESCROW_STATE_RELEASED {
		return fmt.Errorf("escrow %s state %s after the contract release, want RELEASED",
			short1(s.escrowID), res.Escrow.State)
	}
	balc := banktypes.NewQueryClient(c.conn)
	sellerBal, err := balc.Balance(context.Background(), &banktypes.QueryBalanceRequest{
		Address: addrOf(s.seller).String(), Denom: s.settleDenom})
	if err != nil {
		return err
	}
	if !sellerBal.Balance.Amount.Equal(math.NewInt(exitPrice)) {
		return fmt.Errorf("seller holds %s after release, want %d %s",
			sellerBal.Balance, exitPrice, shortDenom(s.settleDenom))
	}
	fmt.Printf("D-079 SMOKE: a real CVM-schema contract released escrow %s with a groth16 proof; seller paid %s ✓\n",
		short1(s.escrowID), sellerBal.Balance)
	return nil
}

// deliverGas is deliverCode() with an explicit gas limit — the contract upload
// needs far more than defaultDemoGas for its tx-size cost alone. Returns the
// DeliverTx code/log (0 = landed) so callers can assert a SPECIFIC rejection.
func (c *cli) deliverGas(priv cryptotypes.PrivKey, gas uint64, msgs ...sdk.Msg) (uint32, string, error) {
	acc, err := c.account(addrOf(priv))
	if err != nil {
		return 0, "", err
	}
	bz, err := c.signTxFeeGas(priv, acc.GetAccountNumber(), acc.GetSequence(), nil, gas, msgs...)
	if err != nil {
		return 0, "", err
	}
	res, err := c.txc.BroadcastTx(context.Background(), &txtypes.BroadcastTxRequest{
		TxBytes: bz, Mode: txtypes.BroadcastMode_BROADCAST_MODE_SYNC,
	})
	if err != nil {
		return 0, "", err
	}
	if res.TxResponse.Code != 0 {
		return res.TxResponse.Code, res.TxResponse.RawLog, nil
	}
	sum := sha256.Sum256(bz)
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	deadline := time.Now().Add(60 * time.Second)
	for {
		got, err := c.txc.GetTx(context.Background(), &txtypes.GetTxRequest{Hash: hash})
		if err == nil && got.TxResponse.Height > 0 {
			return got.TxResponse.Code, got.TxResponse.RawLog, nil
		}
		if time.Now().After(deadline) {
			return 0, "", fmt.Errorf("tx %s not included before deadline", hash)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// execContract sends one ExecuteMsg to the demo contract and returns the
// DeliverTx code/log. The contract stamps nothing: wasmd supplies the calling
// contract's address as the signer of every encoded escrow msg (D-079 layer 3).
func (c *cli) execContract(s *exitState, msg map[string]any) (uint32, string, error) {
	bz, err := json.Marshal(msg)
	if err != nil {
		return 0, "", err
	}
	return c.deliverCode(s.deployer, sdk.Coins{}, &wasmtypes.MsgExecuteContract{
		Sender: addrOf(s.deployer).String(), Contract: s.contract, Msg: bz,
	})
}

// ---- stage 6: the evidence, including what it does NOT prove ---------------

func (c *cli) exitStageEvidence(s *exitState) error {
	ev, err := c.gatherExitEvidence(s)
	if err != nil {
		return err
	}
	fmt.Printf("receipt %s + ICS-23 proof at height %d; terms_hash recomputes from the queried winning Solution ✓\n",
		short1(ev.receipt.Id), ev.proof.ProofHeight)
	return c.exitAssertConditionBoundary(s, ev)
}

// exitEvidence is the fill-back evidence bundle an origin settler is handed
// (7683_SURFACE §6.5 step 4): the receipt, its ICS-23 proof from the x/receipt
// product surface, and the winning Match pre-image FETCHED from the stored
// Solution (never recomputed).
type exitEvidence struct {
	receipt *receipttypes.Receipt
	proof   *receipttypes.QueryReceiptProofResponse
	match   *sessiontypes.Match
	matchBz []byte
}

// gatherExitEvidence collects that bundle and runs §6.3 check 2 (terms
// authenticity) over it. Shared by §6a's stage 6 and §6b's stage 5 so both
// verify the SAME bundle by the same rules — §6b only re-roots check 1 in a
// second chain's light client.
func (c *cli) gatherExitEvidence(s *exitState) (*exitEvidence, error) {
	ctx := context.Background()
	recs, err := c.receiptc.Receipts(ctx, &receipttypes.QueryReceiptsRequest{SigilId: s.sigilP})
	if err != nil {
		return nil, err
	}
	var receipt *receipttypes.Receipt
	for i := range recs.Receipts {
		if recs.Receipts[i].IntentId == s.buyID {
			receipt = &recs.Receipts[i]
		}
	}
	if receipt == nil {
		return nil, fmt.Errorf("no receipt for the buy intent %s", short1(s.buyID))
	}
	proof, err := c.receiptc.ReceiptProof(ctx, &receipttypes.QueryReceiptProofRequest{Id: receipt.Id})
	if err != nil {
		return nil, fmt.Errorf("receipt.ReceiptProof: %w", err)
	}
	if len(proof.Proof) == 0 || proof.ProofHeight == 0 {
		return nil, fmt.Errorf("empty ICS-23 proof for receipt %s", short1(receipt.Id))
	}

	// 7683_SURFACE §6.3 check 2 — terms authenticity — over the winning
	// Solution's Match, fetched (never recomputed).
	sres, err := c.sessionc.Session(ctx, &sessiontypes.QuerySessionRequest{Id: s.sessionID})
	if err != nil {
		return nil, err
	}
	sols, err := c.sessionc.Solutions(ctx, &sessiontypes.QuerySolutionsRequest{SessionId: s.sessionID})
	if err != nil {
		return nil, err
	}
	var winning *sessiontypes.Solution
	for i := range sols.Solutions {
		if sols.Solutions[i].Id == sres.Session.WinningSolutionId {
			winning = &sols.Solutions[i]
		}
	}
	if winning == nil {
		return nil, fmt.Errorf("winning solution %s not queryable", short1(sres.Session.WinningSolutionId))
	}
	matchBz, err := winning.Matches[0].Marshal()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(matchBz)
	if !bytes.Equal(sum[:], receipt.TermsHash) {
		return nil, fmt.Errorf("§6.3(2) terms_hash does not recompute from the queried Match")
	}
	// §6.3 check 2's second half: the receipt must belong to this Match.
	if !slices.Contains(winning.Matches[0].IntentIds, receipt.IntentId) {
		return nil, fmt.Errorf("§6.3(2) receipt.intent_id %s is not in the verified Match",
			short1(receipt.IntentId))
	}
	return &exitEvidence{receipt: receipt, proof: proof, match: &winning.Matches[0], matchBz: matchBz}, nil
}

// exitAssertConditionBoundary demonstrates the D-087 boundary rather than
// avoiding it, and is shared by both halves of the demo.
func (c *cli) exitAssertConditionBoundary(s *exitState, ev *exitEvidence) error {
	ctx := context.Background()
	receipt, winningMatch := ev.receipt, ev.match
	// This settlement IS conditioned, so:
	//   (i)  its receipt still reads condition_outcome=NONE — the field is
	//        decorative until task 2.9;
	//   (ii) 7683_SURFACE §6.3 check 3 requires an UNCONDITIONED leg, so this
	//        receipt is correctly NOT admissible as delivery evidence on the v1
	//        fill-back rail. The demo asserts the check REJECTS it.
	// Delivery is proven by the escrow's terminal state instead.
	if receipt.ConditionOutcome != receipttypes.CONDITION_OUTCOME_NONE {
		return fmt.Errorf("condition_outcome is %s — D-087 has been fixed; update this demo and 2.9's status",
			receipt.ConditionOutcome)
	}
	admissible := false
	for i := range winningMatch.Legs {
		leg := &winningMatch.Legs[i]
		if leg.ToSigil == s.sigilP && leg.Condition == nil && leg.GetData() != nil {
			admissible = true
		}
	}
	if admissible {
		return fmt.Errorf("§6.3(3) admitted a conditioned delivery leg — the bridge rule is not being enforced")
	}
	fmt.Println("D-087 boundary shown, not avoided:")
	fmt.Println("  · receipt.condition_outcome = NONE even though this settlement IS conditioned (task 2.9)")
	fmt.Println("  · §6.3 check 3 REJECTS the conditioned delivery leg — this receipt is correctly")
	fmt.Println("    inadmissible as delivery evidence on the v1 fill-back rail")

	escrowc := escrowtypes.NewQueryClient(c.conn)
	eres, err := escrowc.Escrow(ctx, &escrowtypes.QueryEscrowRequest{Id: s.escrowID})
	if err != nil {
		return err
	}
	if eres.Escrow.State != escrowtypes.ESCROW_STATE_RELEASED {
		return fmt.Errorf("escrow %s is %s", short1(s.escrowID), eres.Escrow.State)
	}
	fmt.Printf("  · delivery IS proven — by escrow %s in terminal state RELEASED ✓\n", short1(s.escrowID))
	return nil
}

// ---- stage 7: the consent gate, from the LIVE path ------------------------

// exitStageConsentNegative is the load-bearing negative for the capability this
// task adds. The demo driver is now the first live-network writer of
// SettleLeg.Condition, so the question Rule 4 asks is: can that writer IMPOSE a
// condition on a party who did not sign it? It must not — and the guard must be
// proven reached from THIS path, not only from the keeper tests.
//
// Shape: the seller signs the ZK condition, the BUYER signs nothing (a plain
// intent), and the proposer attaches the condition anyway. That is exactly the
// D-076 buyer-side hole, and validateSolution must reject the whole solution
// (ErrInvalidSolution carrying ErrConditionNotConsented) and slash the
// over-reaching solver 5%.
//
// RUN LAST, AND SINCE TASK 2.17 THAT IS LOAD-BEARING RATHER THAN CAUTIOUS.
// The rationale was always "the slash takes solver1's bond below
// solver_min_bond, so nothing after it may need a solution" — but until 2.17
// the slash was reverted with the failing tx, so the ordering constrained
// nothing and this stage could have been moved anywhere without any run
// noticing. It now really does take 5% of the bond, on a live node, before
// every later stage. DO NOT REORDER THIS STAGE without re-checking what
// follows it: the reason is no longer hypothetical.
//
// It also asserts the WIRE IDENTITY of that rejection (task 2.13/D-093): the
// (codespace, code) pair an off-chain solver dispatches on. This was D-091's
// finding F-1 — the registered code was lost to errors.Join and the rejection
// arrived as the internal code 1 — and the assertion below is the live proof
// the fix reaches the wire, not merely the keeper.
func (c *cli) exitStageConsentNegative(s *exitState) error {
	height, err := c.height()
	if err != nil {
		return err
	}
	ttl := height + settleTTL
	// the buyer signs NO condition; the seller signs the ZK one
	buy := c.buildIntent(addrOf(s.gateway), s.sigilP,
		[]veiltypes.IntentLeg{coinLeg(s.settleDenom, exitPrice)},
		[]veiltypes.IntentLeg{dataLeg(exitDataset)}, ttl)
	sell := c.buildIntentCond(addrOf(s.seller), s.sigilS,
		[]veiltypes.IntentLeg{dataLeg(exitDataset)},
		[]veiltypes.IntentLeg{coinLeg(s.settleDenom, exitPrice)}, ttl, s.cond)
	ids, err := c.submitConcurrent([]txSpec{{priv: s.gateway, msg: buy}, {priv: s.seller, msg: sell}})
	if err != nil {
		return err
	}
	if err := c.waitSameSession(ids); err != nil {
		return err
	}
	rec, err := c.intentRecord(ids[0])
	if err != nil {
		return err
	}
	if err := c.waitForAuction(rec.SessionId); err != nil {
		return err
	}
	// Read the solver's bond BEFORE the rejection, so the slash below is a
	// measured DELTA rather than an absolute compared against an assumption
	// about what earlier stages left behind.
	before, err := c.solverState(s.solver)
	if err != nil {
		return err
	}
	code, space, log, err := c.proposeMatch(s.solver, rec.SessionId,
		conditionedMatch(ids[0], ids[1], s.sigilP, s.sigilS, s.cond, s.settleDenom))
	if err != nil {
		return err
	}
	if code == 0 {
		return fmt.Errorf("CONSENT GATE BROKEN: a solution imposed a ZK condition the buyer never signed (D-076)")
	}
	// The REASON must be the consent gate specifically — a rejection for some
	// other reason would prove nothing about D-076.
	if !strings.Contains(log, "did not sign this ZK condition") {
		return fmt.Errorf("unsigned-condition solution rejected for the WRONG reason (code %d): %s", code, log)
	}
	fmt.Println("consent gate reached from the live path: the proposer CANNOT impose an unsigned")
	fmt.Printf("  ZK condition — solution rejected ✓\n")

	// THE SLASH, ASSERTED RATHER THAN ANNOUNCED (task 2.17). This line used to
	// print "and the solver slashed 5% ✓" with nothing behind it — and it was
	// FALSE for every run that ever printed it: the slash was written by the msg
	// server and reverted with the failing tx, so no live run has ever taken a
	// solver's bond (D-097 finding F-1). A banner asserting an outcome it never
	// queried is the positive form of the vacuous negative: it "passes" whatever
	// the chain does, which is why it survived two phases saying something untrue.
	//
	// Now the bond is read back and the SPEC §6.4 rate checked exactly. If the
	// ante decorator is ever unwired, THIS is what fails, on a live node.
	if err := c.assertProposalSlash(before); err != nil {
		return err
	}

	// WIRE-IDENTITY ASSERTION (task 2.13 / D-093, replacing D-091's F-1 boundary
	// pin). An off-chain solver dispatches on the (codespace, code) PAIR, so both
	// halves are asserted: a bare code is ambiguous across modules, and a bare
	// codespace does not distinguish this rejection from any other session error.
	//
	// Before 2.13 this arrived as code 1 / codespace "" — ProposeSolution
	// returned errors.Join(ErrInvalidSolution, ruleErr) and cosmossdk.io/errors
	// cannot walk a multi-error Unwrap() []error, so the registered code was
	// dropped and every invalid solution looked like an internal error. The pin
	// that recorded that boundary was watched RED against the fixed tree before
	// being replaced by these assertions.
	if code != sessiontypes.ErrInvalidSolution.ABCICode() {
		return fmt.Errorf("wire code is %d, want session %d (ErrInvalidSolution) — "+
			"the registered code is not reaching the wire (D-091 F-1 / task 2.13): %s",
			code, sessiontypes.ErrInvalidSolution.ABCICode(), firstLine(log))
	}
	if space != sessiontypes.ModuleName {
		return fmt.Errorf("wire codespace is %q, want %q — a client cannot dispatch on the "+
			"documented SPEC §13 codespace (D-091 F-1 / task 2.13)", space, sessiontypes.ModuleName)
	}
	fmt.Printf("  wire identity: codespace %q code %d (ErrInvalidSolution) — an off-chain solver\n", space, code)
	fmt.Println("  can dispatch on the documented SPEC §13 codespace, and the rule text survives ✓")
	return nil
}

// solverState reads a solver's registry record (bond + slashed_count).
func (c *cli) solverState(priv cryptotypes.PrivKey) (sessiontypes.Solver, error) {
	res, err := c.sessionc.Solver(context.Background(),
		&sessiontypes.QuerySolverRequest{Addr: addrOf(priv).String()})
	if err != nil {
		return sessiontypes.Solver{}, fmt.Errorf("query solver: %w", err)
	}
	return res.Solver, nil
}

// assertProposalSlash checks that the SPEC §6.4 invalid-solution slash actually
// moved the solver's bond, at the governed rate (task 2.17).
//
// The rate is read from chain PARAMS rather than hardcoded at 5%: it is
// governable (invalid_solution_slash_bps, D-007/D-008), so a demo asserting a
// literal would start lying the first time governance changed it — the same
// class of error this assertion replaces.
func (c *cli) assertProposalSlash(before sessiontypes.Solver) error {
	ctx := context.Background()
	params, err := c.sessionc.Params(ctx, &sessiontypes.QueryParamsRequest{})
	if err != nil {
		return fmt.Errorf("query session params: %w", err)
	}
	bps := params.Params.InvalidSolutionSlashBps

	after, err := c.sessionc.Solver(ctx, &sessiontypes.QuerySolverRequest{Addr: before.Addr})
	if err != nil {
		return fmt.Errorf("read solver back after the rejection: %w", err)
	}
	if after.Solver.SlashedCount != before.SlashedCount+1 {
		return fmt.Errorf("SPEC §6.4 SLASH DID NOT LAND: slashed_count %d → %d. "+
			"Task 2.17 moved this slash into the ante (ProposeSolutionSlashDecorator) "+
			"because a msg-server write is discarded with the failing tx; if this fires, "+
			"that decorator is not in the chain",
			before.SlashedCount, after.Solver.SlashedCount)
	}
	want := sdk.NewCoins()
	for _, coin := range before.Bond {
		cut := coin.Amount.MulRaw(int64(bps)).QuoRaw(10_000)
		if rem := coin.Amount.Sub(cut); rem.IsPositive() {
			want = want.Add(sdk.NewCoin(coin.Denom, rem))
		}
	}
	if !after.Solver.Bond.Equal(want) {
		return fmt.Errorf("SPEC §6.4 SLASH AMOUNT WRONG: bond is %s, want %s "+
			"(invalid_solution_slash_bps = %d)", after.Solver.Bond, want, bps)
	}
	fmt.Printf("  and the solver's bond REALLY fell: %s → %s at %d bps ✓\n",
		before.Bond, after.Solver.Bond, bps)
	return nil
}

// buildIntentCond is buildIntent with a signed IntentPayload.condition — the
// consent artifact both the D-075 (seller) and D-076 (buyer) gates check the
// solver-attached SettleLeg.Condition against.
func (c *cli) buildIntentCond(sender sdk.AccAddress, sigil string, give, want []veiltypes.IntentLeg,
	ttl uint64, cond *escrowtypes.DeliveryCondition) *veiltypes.MsgSubmitIntent {
	msg := c.buildIntent(sender, sigil, give, want, ttl)
	payload := veiltypes.IntentPayload{Give: give, Want: want, Condition: cond}
	bz, err := payload.Marshal()
	if err != nil {
		panic(err)
	}
	msg.Ciphertext = bz
	return msg
}
