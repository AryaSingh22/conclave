// exit-demo-ibc — the live driver for the Phase-2 §6 exit demo, part B (§6b).
//
// WHAT §6b ADDS TO §6a. §6a proved x402 ingress + a ZK escrow + a CVM contract
// release on ONE chain settling in literal uusdc. §6b adds the fourth leg and
// asserts the SPEC §10 exit criterion:
//
//	stage 1   a REAL ICS-20 uusdc transfer from chain B to chain A, moved by a
//	          LIVE RELAYER process (D-082's obligation — explicitly NOT the
//	          in-process ibctesting harness 2.5 used). Chain A mints an
//	          ibc/<hash> voucher whose trace resolves to chain B's uusdc.
//	stages
//	2 - 4     §6a's whole flow re-run with that VOUCHER as the settlement denom:
//	          the gateway's ceilings and fee_denoms, the buy's give leg, the
//	          escrow's held amount and the seller's payout are all ibc/<hash>.
//	stage 5   the receipt's ICS-23 proof verified against the root held by
//	          CHAIN B's on-chain light client OF CHAIN A.
//
// THE HONEST CLAIM — the two directions are NOT equally strong, and this file
// is careful about which is which:
//
//	B → A   IS verified by a state machine. The voucher can only exist because
//	        chain A executed MsgRecvPacket, and ibc-go's OnRecvPacket runs an
//	        ICS-23 verification of chain B's packet commitment against chain A's
//	        ON-CHAIN light client of B. That is a real in-consensus
//	        cross-chain verification and stage 1 says so.
//	A → B   is verified IN THIS DRIVER, against chain B's root. The §6.3 checks
//	        execute here because the settler/bridge is separate-repo scope
//	        (7683_SURFACE §1.2, §6.3). What makes it a SECOND-CHAIN
//	        verification is that the ROOT is read out of chain B's stored
//	        ConsensusState for its client of chain A — established by a live
//	        relayer and advanced by real MsgUpdateClient txs on B — instead of
//	        out of chain A's own header, which is what §6a used.
//
// It is NOT claimed that chain B's state machine verified the receipt. Making
// that true means deploying the verification bridge (a contract on B running
// ICS-23 against its client), which is resolver-repo scope by contract.
//
// Dev tooling only — not consensus code.
package main

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"cosmossdk.io/math"

	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	ibcconnectiontypes "github.com/cosmos/ibc-go/v8/modules/core/03-connection/types"
	ibcchanneltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	commitmenttypes "github.com/cosmos/ibc-go/v8/modules/core/23-commitment/types"
	ibctmtypes "github.com/cosmos/ibc-go/v8/modules/light-clients/07-tendermint"

	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
)

const (
	transferPort = "transfer"
	// one ICS-20 packet carries the principal's settlement float plus the
	// gateway's gas float; the principal forwards the gateway's cut on chain A.
	ibcSendAmount = 2_500 * usdc
	ibcGatewayCut = 500 * usdc
	// how long to wait for a LIVE relayer to move a packet. Generous: it covers
	// client updates, the packet, and its acknowledgement.
	ibcRelayTimeout = 150 * time.Second
)

// ibcLeg carries §6b's cross-chain artifacts. Nil on a §6a (native) run.
type ibcLeg struct {
	b *cli // chain B: the ICS-20 uusdc source AND the host of the light client of chain A

	chanA, chanB string // the ICS-20 channel ends
	// clientOnA is chain A's light client OF CHAIN B — the one MsgRecvPacket
	// verified against in stage 1 (the direction that runs in consensus).
	clientOnA string
	// clientOnB is chain B's light client OF CHAIN A — the trust root stage 5
	// verifies the receipt against (the direction that runs in this driver).
	clientOnB string

	voucher string // the ibc/<hash> denom as minted on chain A
	trace   string // its resolved trace path, e.g. "transfer/channel-0"
}

func (c *cli) exitDemoIBC(b *cli) error {
	fmt.Println("=== Conclave Phase-2 §6 EXIT DEMO — part B (§6b) ===")
	fmt.Println("=== THE SPEC §10 EXIT CRITERION IS ASSERTED HERE.")
	fmt.Println("=== INTERNAL ACCEPTANCE RUN (D-090): the settlement is CONDITIONED, so its")
	fmt.Println("=== receipt reads condition_outcome=NONE (D-087). Task 2.9 is a PREREQUISITE")
	fmt.Println("=== if this output is ever repurposed as external-facing evidence.")
	fmt.Printf("=== chain A = %s (settlement)   chain B = %s (USDC source + light client of A)\n",
		c.chainID, b.chainID)
	fmt.Println()

	s := &exitState{ibc: &ibcLeg{b: b}}
	for _, stage := range []struct {
		name string
		run  []func(*exitState) error
	}{
		{"stage 1 — IBC USDC in: real uusdc from chain B over a LIVE RELAYER",
			[]func(*exitState) error{c.exitStageActors, c.exitStageIBCIn}},
		{"stage 2 — x402 ingress: the conditioned ZK buy, settling in the voucher",
			[]func(*exitState) error{c.exitStageMandate, c.exitStageBuy}},
		{"stage 3 — conditioned settlement: the ZK escrow opens, holding the voucher",
			[]func(*exitState) error{c.exitStageSettle}},
		{"stage 4 — the CVM contract releases the escrow (D-079), paying in the voucher",
			[]func(*exitState) error{c.exitStageUpload, c.exitStageRelease}},
		{"stage 5 — the receipt, verified against chain B's on-chain light client of chain A",
			[]func(*exitState) error{c.exitStageSecondChainVerify}},
		{"stage 6 — the consent gate, reached from the live path",
			[]func(*exitState) error{c.exitStageConsentNegative}},
	} {
		fmt.Printf("---- %s ----\n", stage.name)
		for _, run := range stage.run {
			if err := run(s); err != nil {
				return fmt.Errorf("%s: %w", stage.name, err)
			}
		}
		fmt.Println()
	}

	fmt.Println("=================================================================")
	fmt.Println(" PHASE-2 §6 EXIT CRITERION — ASSERTED")
	fmt.Println("   an external x402 agent bought ZK-verified data from a Conclave")
	fmt.Println("   agent, settling in real IBC USDC, and the receipt verified")
	fmt.Println("   against a second Cosmos chain's on-chain light client.")
	fmt.Println()
	fmt.Println(" Boundaries (docs/EXIT_DEMO.md states each one):")
	fmt.Println("   · B→A was verified IN CONSENSUS by chain A (MsgRecvPacket, ICS-23).")
	fmt.Println("   · A→B was verified in THIS DRIVER against chain B's light-client")
	fmt.Println("     root — the settler is separate-repo scope (7683_SURFACE §6.3).")
	fmt.Println("     NOT claimed: chain B's state machine verified the receipt.")
	fmt.Println("   · INTERNAL run: condition_outcome=NONE stands until task 2.9.")
	fmt.Println("   · Chain A is single-node, so the Veil is in its epoch-0 plaintext")
	fmt.Println("     bootstrap window (D-070 owns the encrypted reveal).")
	fmt.Println("   · Conditioned escrows remain reachable live only through demo")
	fmt.Println("     tooling (task 2.13) — the network still has no such writer.")
	fmt.Println("=================================================================")
	return nil
}

// ---- stage 1: the IBC leg --------------------------------------------------

func (c *cli) exitStageIBCIn(s *exitState) error {
	ctx := context.Background()
	leg := s.ibc
	b := leg.b

	// (1) The channel must be OPEN on BOTH sides and the two ends must name
	// each other. Querying one side only would accept a half-open handshake.
	chA, err := openTransferChannel(ctx, c)
	if err != nil {
		return fmt.Errorf("chain A (%s): %w", c.chainID, err)
	}
	chB, err := openTransferChannel(ctx, b)
	if err != nil {
		return fmt.Errorf("chain B (%s): %w", b.chainID, err)
	}
	if chA.Counterparty.ChannelId != chB.ChannelId || chB.Counterparty.ChannelId != chA.ChannelId {
		return fmt.Errorf("the two channel ends do not name each other: A %s→%s, B %s→%s",
			chA.ChannelId, chA.Counterparty.ChannelId, chB.ChannelId, chB.Counterparty.ChannelId)
	}
	leg.chanA, leg.chanB = chA.ChannelId, chB.ChannelId

	// (2) Resolve each side's light client OF THE OTHER through the very
	// connection this channel travels — not by scanning for "some" client. The
	// client's own ClientState.chain_id is checked, so a client of the wrong
	// chain cannot be mistaken for the right one.
	if leg.clientOnA, err = clientOfCounterparty(ctx, c, chA, b.chainID); err != nil {
		return fmt.Errorf("chain A's light client of %s: %w", b.chainID, err)
	}
	if leg.clientOnB, err = clientOfCounterparty(ctx, b, chB, c.chainID); err != nil {
		return fmt.Errorf("chain B's light client of %s: %w", c.chainID, err)
	}
	fmt.Printf("ICS-20 channel OPEN on BOTH chains: A %s/%s ↔ B %s/%s ✓\n",
		transferPort, leg.chanA, transferPort, leg.chanB)
	fmt.Printf("  A's light client of B: %s (stage 1 verifies against this, in consensus)\n", leg.clientOnA)
	fmt.Printf("  B's light client of A: %s (stage 5's trust root)\n", leg.clientOnB)

	// (3) The transfer. Tokens are ESCROWED on chain B and minted as a voucher
	// on chain A — asserted on both sides, so this is a real cross-chain move
	// and not a local mint.
	faucetB, err := b.key("faucet")
	if err != nil {
		return fmt.Errorf("chain B faucet key: %w", err)
	}
	amount := sdk.NewInt64Coin(uusdc, ibcSendAmount)
	escrowAddr := ibctransfertypes.GetEscrowAddress(transferPort, leg.chanB)
	escBefore, err := balanceOf(ctx, b, escrowAddr.String(), uusdc)
	if err != nil {
		return err
	}
	transfer := ibctransfertypes.NewMsgTransfer(transferPort, leg.chanB, amount,
		addrOf(faucetB).String(), addrOf(s.principal).String(),
		ibcclienttypes.ZeroHeight(), uint64(time.Now().Add(10*time.Minute).UnixNano()), "")
	if err := b.deliver(faucetB, transfer); err != nil {
		return fmt.Errorf("MsgTransfer on chain B: %w", err)
	}
	fmt.Printf("chain B sent %s to the buyer on chain A; waiting for the LIVE relayer\n", amount)

	// (4) Wait for the relayer. A timeout here is the single most informative
	// failure this demo can produce — see the message.
	voucher, got, err := c.waitForVoucher(ctx, addrOf(s.principal).String())
	if err != nil {
		return err
	}
	if !got.Equal(amount.Amount) {
		return fmt.Errorf("buyer received %s of %s, want %s", got, shortDenom(voucher), amount.Amount)
	}

	// (5) The voucher must be chain B's uusdc — NOT chain A's native uusdc,
	// which also exists and would silently satisfy a weaker check.
	if voucher == uusdc {
		return fmt.Errorf("settlement denom is the chain's OWN uusdc — no IBC asset was used")
	}
	tr, err := ibctransfertypes.NewQueryClient(c.conn).DenomTrace(ctx,
		&ibctransfertypes.QueryDenomTraceRequest{Hash: strings.TrimPrefix(voucher, "ibc/")})
	if err != nil {
		return fmt.Errorf("transfer.DenomTrace(%s): %w", shortDenom(voucher), err)
	}
	wantPath := transferPort + "/" + leg.chanA
	if tr.DenomTrace.BaseDenom != uusdc || tr.DenomTrace.Path != wantPath {
		return fmt.Errorf("denom trace resolves to %q over %q, want base %q over %q",
			tr.DenomTrace.BaseDenom, tr.DenomTrace.Path, uusdc, wantPath)
	}
	if tr.DenomTrace.IBCDenom() != voucher {
		return fmt.Errorf("trace does not recompute the voucher denom: %s vs %s",
			tr.DenomTrace.IBCDenom(), voucher)
	}
	leg.voucher, leg.trace = voucher, tr.DenomTrace.Path

	// (6) Conservation across the pair: chain B escrowed exactly what chain A minted.
	escAfter, err := balanceOf(ctx, b, escrowAddr.String(), uusdc)
	if err != nil {
		return err
	}
	if !escAfter.Sub(escBefore).Equal(amount.Amount) {
		return fmt.Errorf("chain B's ICS-20 escrow moved %s, want %s", escAfter.Sub(escBefore), amount.Amount)
	}

	// (7) The gateway needs the voucher too — it fronts gas in the settlement denom.
	if err := c.deliver(s.principal, &banktypes.MsgSend{
		FromAddress: addrOf(s.principal).String(),
		ToAddress:   addrOf(s.gateway).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(voucher, ibcGatewayCut)),
	}); err != nil {
		return fmt.Errorf("fund the gateway with the voucher: %w", err)
	}

	s.settleDenom = voucher
	fmt.Printf("D-082 DISCHARGED: buyer holds %s %s\n", got, voucher)
	fmt.Printf("  trace: %s/%s → chain B's uusdc, escrowed on B (%s) ✓\n",
		tr.DenomTrace.Path, tr.DenomTrace.BaseDenom, amount)
	fmt.Println("  this voucher exists ONLY because chain A executed MsgRecvPacket, which")
	fmt.Printf("  ICS-23-verified chain B's packet commitment against client %s — a real\n", leg.clientOnA)
	fmt.Println("  in-consensus cross-chain verification, performed by chain A's state machine ✓")
	return nil
}

// waitForVoucher polls chain A until an ibc/<hash> balance appears for addr.
func (c *cli) waitForVoucher(ctx context.Context, addr string) (string, math.Int, error) {
	balc := banktypes.NewQueryClient(c.conn)
	deadline := time.Now().Add(ibcRelayTimeout)
	for {
		res, err := balc.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{Address: addr})
		if err == nil {
			for _, coin := range res.Balances {
				if strings.HasPrefix(coin.Denom, "ibc/") {
					return coin.Denom, coin.Amount, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return "", math.Int{}, fmt.Errorf(
				"the ICS-20 packet was NOT relayed within %s: chain B accepted the MsgTransfer, but no\n"+
					"  ibc/<hash> voucher ever appeared on chain A.\n"+
					"  DO NOT assume ports/config first. A relayer discovers packets from block_results\n"+
					"  events, and THIS chain prepends an injected tx at Txs[0] (task 1.6's vote-extension\n"+
					"  transport). If the relayer is not seeing our blocks or their events, that is a\n"+
					"  finding about Conclave's compatibility with standard IBC tooling — every relayer\n"+
					"  operator would hit it — and it must be reported as its own task, NOT patched around\n"+
					"  here. Read the relayer log for packet discovery before touching this script.",
				ibcRelayTimeout)
		}
		time.Sleep(time.Second)
	}
}

// ---- stage 5: verification against chain B's light client ------------------

func (c *cli) exitStageSecondChainVerify(s *exitState) error {
	ctx := context.Background()
	leg := s.ibc
	b := leg.b

	// The evidence bundle and §6.3 check 2 (terms authenticity) — identical to
	// §6a's, over the same shared code.
	ev, err := c.gatherExitEvidence(s)
	if err != nil {
		return err
	}
	fmt.Printf("evidence: receipt %s, x/receipt ICS-23 proof at height %d, Match pre-image %dB\n",
		short1(ev.receipt.Id), ev.proof.ProofHeight, len(ev.matchBz))
	fmt.Println("  §6.3(2) terms_hash recomputes from the queried winning Solution ✓")

	// Chain B's light client only knows heights it was UPDATED to, and with no
	// A→B traffic it has no reason to advance. So drive real traffic: relaying
	// a packet forces MsgUpdateClient on B with a header at least as new as the
	// packet's commitment, which is newer than our receipt.
	if err := c.forceClientAdvance(s, ev.proof.ProofHeight); err != nil {
		return err
	}

	// LIGHT-CLIENT-HEIGHT-FIRST (the inversion): read the height chain B's
	// client actually holds, THEN ask chain A for a proof at the version that
	// height's root commits to. The alternative — proof first, then hoping the
	// relayer lands an update at exactly ProofHeight+1 — is a race over a
	// header height we do not choose.
	//
	// A ConsensusState at height H stores Root = A.header(H).AppHash, and a
	// header's AppHash commits the app state as of the PREVIOUS block. So the
	// proof must be taken at version H-1. That off-by-one is the thing most
	// likely to be silently wrong, so it is also asserted as a negative below.
	heights, err := consensusHeights(ctx, b, leg.clientOnB)
	if err != nil {
		return err
	}
	var chosen ibcclienttypes.Height
	for _, h := range heights {
		if h.RevisionHeight >= ev.proof.ProofHeight+1 {
			chosen = h
			break
		}
	}
	if chosen.RevisionHeight == 0 {
		return fmt.Errorf("chain B's client %s holds no consensus state at or after height %d (has %d heights) — "+
			"the relayer never advanced it past the receipt", leg.clientOnB, ev.proof.ProofHeight+1, len(heights))
	}
	root, err := consensusRoot(ctx, b, leg.clientOnB, chosen)
	if err != nil {
		return err
	}
	version := int64(chosen.RevisionHeight) - 1
	fmt.Printf("chain B's client %s holds chain A's root at height %d (%d stored heights)\n",
		leg.clientOnB, chosen.RevisionHeight, len(heights))
	fmt.Printf("  → that root commits chain A's state at version %d; fetching the proof there\n", version)

	// The height-pinned proof, over the generic ABCI route 7683_SURFACE §6.2
	// documents. x/receipt's ReceiptProof always proves at the TIP (it passes
	// no Height), so the product surface cannot serve a chosen height — its
	// proof is still fetched above, and the two are cross-checked below.
	ridBz, err := sessiontypes.IDBytes(ev.receipt.Id)
	if err != nil {
		return err
	}
	key := sessiontypes.ReceiptByIDKey(ridBz)
	res, err := c.cmtc.ABCIQuery(ctx, &cmtservice.ABCIQueryRequest{
		Path:   "/store/" + sessiontypes.StoreKey + "/key",
		Data:   key,
		Height: version,
		Prove:  true,
	})
	if err != nil {
		return fmt.Errorf("historical store query at version %d: %w", version, err)
	}
	if res.Code != 0 {
		return fmt.Errorf("historical store query at version %d: code %d: %s", version, res.Code, res.Log)
	}
	if res.ProofOps == nil || len(res.ProofOps.Ops) == 0 {
		return fmt.Errorf(
			"chain A returned NO proof at version %d — that version is pruned.\n"+
				"  This is contract-relevant, not demo plumbing: a light-client-driven bridge must fetch\n"+
				"  proofs at the heights its client actually holds, so any node serving this rail has to\n"+
				"  retain those versions. The demo nodes run pruning=nothing for exactly this reason.",
			version)
	}
	if res.Height != version {
		return fmt.Errorf("asked chain A for version %d, it served %d", version, res.Height)
	}
	// The product surface and the historical route must prove the SAME bytes.
	wantBz, err := ev.proof.Receipt.Marshal()
	if err != nil {
		return err
	}
	if !bytes.Equal(res.Value, wantBz) {
		return fmt.Errorf("the historical proof commits different bytes than x/receipt's ReceiptProof returned")
	}

	// The provenance claim — "this root came from the SECOND CHAIN" — is about
	// WHERE the bytes were read, and a value check cannot prove that on its own:
	// a faithful light client holds exactly the same bytes chain A's own header
	// carries. So assert the AGREEMENT explicitly. It proves the relayer has
	// been feeding chain B real chain-A headers (a client tracking something
	// else, or a stale/forged update, shows up here), and makes the claim
	// concrete:
	// identical roots obtained independently from two chains.
	hdr, err := c.cmtc.GetBlockByHeight(ctx, &cmtservice.GetBlockByHeightRequest{
		Height: int64(chosen.RevisionHeight)})
	if err != nil {
		return fmt.Errorf("chain A header at %d: %w", chosen.RevisionHeight, err)
	}
	if !bytes.Equal(hdr.SdkBlock.Header.AppHash, root) {
		return fmt.Errorf("chain B's light client holds root %X for height %d, but chain A's own header "+
			"there carries %X — the client is NOT tracking chain A faithfully",
			root, chosen.RevisionHeight, hdr.SdkBlock.Header.AppHash)
	}

	// §6.3 check 1 — root authenticity — rooted in the SECOND CHAIN.
	if err := verifyAgainstRoot(root, key, res.Value, res.ProofOps); err != nil {
		return fmt.Errorf("§6.3(1) against chain B's light-client root: %w", err)
	}
	fmt.Printf("§6.3(1) receipt ICS-23-VERIFIED against chain B's stored root %X… ✓\n", root[:8])
	fmt.Println("  the root was read from chain B's on-chain ConsensusState — NOT from chain A's own")
	fmt.Printf("  header — and it AGREES byte-for-byte with chain A's header at %d, so chain B's\n",
		chosen.RevisionHeight)
	fmt.Println("  light client is demonstrably tracking chain A ✓")

	// NEGATIVE 1 — a tampered receipt must fail, and fail AS A MEMBERSHIP
	// failure. Asserting merely "an error" would pass on a malformed-proof or
	// wrong-root error and prove nothing (the §6a F-3 lesson).
	tampered := slices.Clone(res.Value)
	tampered[len(tampered)-1] ^= 0x01
	err = verifyAgainstRoot(root, key, tampered, res.ProofOps)
	if err == nil {
		return fmt.Errorf("NEGATIVE FAILED: a tampered receipt verified against chain B's root")
	}
	if !strings.Contains(err.Error(), "chained membership proof failed to verify membership of value") {
		return fmt.Errorf("tampered receipt rejected for the WRONG reason: %w", err)
	}
	fmt.Println("negative: a one-byte-tampered receipt fails ICS-23 membership specifically ✓")

	// NEGATIVE 2 — a wrong-height root. The SAME proof checked against the root
	// at the NEAREST other stored height must fail, and it must fail at the
	// FINAL root comparison ("did not commit to expected root") rather than at
	// membership — a different message from negative 1, which is what proves
	// the two negatives are testing different things and not each other.
	//
	// Nearest, not any: a root from far away is an easy reject and would let an
	// adjacent-height confusion pass unnoticed.
	other, ok := nearestOtherHeight(heights, chosen)
	if !ok {
		return fmt.Errorf("chain B's client holds only one consensus height — the wrong-height negative " +
			"cannot run, so the height binding would be UNPROVEN")
	}
	otherRoot, err := consensusRoot(ctx, b, leg.clientOnB, other)
	if err != nil {
		return err
	}
	err = verifyAgainstRoot(otherRoot, key, res.Value, res.ProofOps)
	if err == nil {
		return fmt.Errorf("NEGATIVE FAILED: the proof verified against the root at height %d as well as %d — "+
			"the height binding is not real", other.RevisionHeight, chosen.RevisionHeight)
	}
	if !strings.Contains(err.Error(), "proof did not commit to expected root") {
		return fmt.Errorf("wrong-height root rejected for the WRONG reason: %w", err)
	}
	fmt.Printf("negative: the same proof against chain B's nearest other root (height %d) fails the root check specifically ✓\n",
		other.RevisionHeight)

	// NEGATIVE 3 — THE OFF-BY-ONE ITSELF, which is the mistake a bridge
	// implementer actually makes: take the proof at the client's height H
	// rather than at H-1, and check it against the root the client holds for H.
	// Both artifacts are internally valid and both name "height H", so nothing
	// looks wrong; it fails only because a header's AppHash commits the
	// PREVIOUS block's state. Negative 2 proves heights are bound at all — this
	// one proves the specific confusion this stage is built to avoid is caught.
	offBy, err := c.cmtc.ABCIQuery(ctx, &cmtservice.ABCIQueryRequest{
		Path:   "/store/" + sessiontypes.StoreKey + "/key",
		Data:   key,
		Height: int64(chosen.RevisionHeight),
		Prove:  true,
	})
	if err != nil || offBy.Code != 0 || offBy.ProofOps == nil {
		return fmt.Errorf("off-by-one probe: could not fetch a proof at version %d: %v (code %d)",
			chosen.RevisionHeight, err, offBy.GetCode())
	}
	err = verifyAgainstRoot(root, key, offBy.Value, offBy.ProofOps)
	if err == nil {
		return fmt.Errorf("NEGATIVE FAILED: a proof taken at version %d verified against the root chain B "+
			"holds for height %d — the H vs H-1 binding is NOT real, and every height in this stage is suspect",
			chosen.RevisionHeight, chosen.RevisionHeight)
	}
	if !strings.Contains(err.Error(), "proof did not commit to expected root") {
		return fmt.Errorf("the off-by-one proof was rejected for the WRONG reason: %w", err)
	}
	fmt.Printf("negative: the off-by-one (proof at version %d vs the root for height %d) fails the root check specifically ✓\n",
		chosen.RevisionHeight, chosen.RevisionHeight)

	// §6.3 check 3 — and it must REJECT this settlement's conditioned leg.
	if err := c.exitAssertConditionBoundary(s, ev); err != nil {
		return err
	}
	// ... and the skip-variant, which is what proves check 3 is load-bearing
	// rather than decorative (7683_SURFACE §7).
	admittedWithoutCheck := false
	for i := range ev.match.Legs {
		if l := &ev.match.Legs[i]; l.ToSigil == s.sigilP && l.GetData() != nil {
			admittedWithoutCheck = true // NOTE: no l.Condition == nil test
		}
	}
	if !admittedWithoutCheck {
		return fmt.Errorf("the §6.3(3) skip-variant admitted nothing — the demonstration is vacuous")
	}
	fmt.Println("  · with check 3 SKIPPED the same conditioned leg IS admitted — the check is load-bearing ✓")

	// §6.3 check 4 — first-claim uniqueness. A bridge obligation, not a node
	// property (the proof is a fact and replays verbatim), so the settler-side
	// ledger is modelled here explicitly.
	claimed := map[string]bool{}
	if err := claimOnce(claimed, ev.receipt.Id); err != nil {
		return fmt.Errorf("the first claim of receipt %s was refused: %w", short1(ev.receipt.Id), err)
	}
	err = claimOnce(claimed, ev.receipt.Id)
	if err == nil {
		return fmt.Errorf("NEGATIVE FAILED: receipt %s was claimed twice", short1(ev.receipt.Id))
	}
	if !strings.Contains(err.Error(), "already claimed") {
		return fmt.Errorf("the replay was refused for the WRONG reason: %w", err)
	}
	fmt.Println("§6.3(4) one-claim-per-receipt_id enforced; the verbatim replay is refused specifically ✓")
	return nil
}

// forceClientAdvance sends real A→B ICS-20 traffic so the live relayer advances
// chain B's light client of chain A past the receipt's height. Relaying a
// packet requires MsgUpdateClient on the destination first, so this is the
// honest way to move the client: with traffic, not with a special command.
func (c *cli) forceClientAdvance(s *exitState, past uint64) error {
	ctx := context.Background()
	leg := s.ibc
	faucetB, err := leg.b.key("faucet")
	if err != nil {
		return err
	}
	// a token amount of the voucher, sent home; enough to make a packet exist
	back := ibctransfertypes.NewMsgTransfer(transferPort, leg.chanA,
		sdk.NewInt64Coin(s.settleDenom, 1), addrOf(s.principal).String(), addrOf(faucetB).String(),
		ibcclienttypes.ZeroHeight(), uint64(time.Now().Add(10*time.Minute).UnixNano()), "")
	if err := c.deliver(s.principal, back); err != nil {
		return fmt.Errorf("return transfer to force a client update: %w", err)
	}
	deadline := time.Now().Add(ibcRelayTimeout)
	for {
		heights, err := consensusHeights(ctx, leg.b, leg.clientOnB)
		if err == nil && len(heights) > 0 && heights[len(heights)-1].RevisionHeight >= past+1 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"chain B's light client of chain A never advanced past height %d within %s.\n"+
					"  The relayer moved the inbound packet but is not updating this client, so the\n"+
					"  second-chain verification has no current root to check against. Investigate the\n"+
					"  relayer's A→B path before assuming a demo-timing problem.", past, ibcRelayTimeout)
		}
		time.Sleep(2 * time.Second)
	}
}

// ---- IBC query helpers -----------------------------------------------------

// openTransferChannel returns the chain's single OPEN ICS-20 channel.
func openTransferChannel(ctx context.Context, x *cli) (ibcchanneltypes.IdentifiedChannel, error) {
	res, err := ibcchanneltypes.NewQueryClient(x.conn).Channels(ctx, &ibcchanneltypes.QueryChannelsRequest{})
	if err != nil {
		return ibcchanneltypes.IdentifiedChannel{}, fmt.Errorf("channel.Channels: %w", err)
	}
	var open []ibcchanneltypes.IdentifiedChannel
	for _, ch := range res.Channels {
		if ch.PortId == transferPort && ch.State == ibcchanneltypes.OPEN {
			open = append(open, *ch)
		}
	}
	switch len(open) {
	case 1:
		return open[0], nil
	case 0:
		return ibcchanneltypes.IdentifiedChannel{}, fmt.Errorf(
			"no OPEN %s channel (%d channels in any state) — the relayer's link did not complete",
			transferPort, len(res.Channels))
	default:
		return ibcchanneltypes.IdentifiedChannel{}, fmt.Errorf(
			"%d OPEN %s channels; the demo expects exactly one", len(open), transferPort)
	}
}

// clientOfCounterparty returns the light client this channel's connection uses,
// after CHECKING that the client really tracks wantChainID.
func clientOfCounterparty(ctx context.Context, x *cli, ch ibcchanneltypes.IdentifiedChannel, wantChainID string) (string, error) {
	if len(ch.ConnectionHops) != 1 {
		return "", fmt.Errorf("channel %s has %d connection hops; the demo expects a direct connection",
			ch.ChannelId, len(ch.ConnectionHops))
	}
	conn, err := ibcconnectiontypes.NewQueryClient(x.conn).Connection(ctx,
		&ibcconnectiontypes.QueryConnectionRequest{ConnectionId: ch.ConnectionHops[0]})
	if err != nil {
		return "", fmt.Errorf("connection.Connection(%s): %w", ch.ConnectionHops[0], err)
	}
	clientID := conn.Connection.ClientId
	cs, err := ibcclienttypes.NewQueryClient(x.conn).ClientState(ctx,
		&ibcclienttypes.QueryClientStateRequest{ClientId: clientID})
	if err != nil {
		return "", fmt.Errorf("client.ClientState(%s): %w", clientID, err)
	}
	var tm ibctmtypes.ClientState
	if err := tm.Unmarshal(cs.ClientState.Value); err != nil {
		return "", fmt.Errorf("client %s is not a tendermint client: %w", clientID, err)
	}
	if tm.ChainId != wantChainID {
		return "", fmt.Errorf("client %s tracks chain %q, want %q", clientID, tm.ChainId, wantChainID)
	}
	return clientID, nil
}

// consensusHeights returns the heights a client actually holds, ascending.
func consensusHeights(ctx context.Context, x *cli, clientID string) ([]ibcclienttypes.Height, error) {
	res, err := ibcclienttypes.NewQueryClient(x.conn).ConsensusStateHeights(ctx,
		&ibcclienttypes.QueryConsensusStateHeightsRequest{ClientId: clientID})
	if err != nil {
		return nil, fmt.Errorf("client.ConsensusStateHeights(%s): %w", clientID, err)
	}
	hs := res.ConsensusStateHeights
	sort.Slice(hs, func(i, j int) bool { return hs[i].RevisionHeight < hs[j].RevisionHeight })
	return hs, nil
}

// consensusRoot reads the app-hash root a light client stores for a height.
func consensusRoot(ctx context.Context, x *cli, clientID string, h ibcclienttypes.Height) ([]byte, error) {
	res, err := ibcclienttypes.NewQueryClient(x.conn).ConsensusState(ctx,
		&ibcclienttypes.QueryConsensusStateRequest{
			ClientId: clientID, RevisionNumber: h.RevisionNumber, RevisionHeight: h.RevisionHeight,
		})
	if err != nil {
		return nil, fmt.Errorf("client.ConsensusState(%s, %s): %w", clientID, h, err)
	}
	var cs ibctmtypes.ConsensusState
	if err := cs.Unmarshal(res.ConsensusState.Value); err != nil {
		return nil, fmt.Errorf("consensus state at %s is not a tendermint one: %w", h, err)
	}
	root := cs.Root.GetHash()
	if len(root) == 0 {
		return nil, fmt.Errorf("chain %s stores an empty root for %s at %s", x.chainID, clientID, h)
	}
	return root, nil
}

// nearestOtherHeight picks the stored height CLOSEST to the chosen one. The
// nearest root is the hardest wrong root to tell apart, so it is the one worth
// asserting against; a distant root would reject trivially.
func nearestOtherHeight(hs []ibcclienttypes.Height, chosen ibcclienttypes.Height) (ibcclienttypes.Height, bool) {
	var best ibcclienttypes.Height
	found := false
	for _, h := range hs {
		if h.RevisionHeight == chosen.RevisionHeight {
			continue
		}
		if !found || absDiff(h.RevisionHeight, chosen.RevisionHeight) < absDiff(best.RevisionHeight, chosen.RevisionHeight) {
			best, found = h, true
		}
	}
	return best, found
}

func absDiff(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}

// verifyAgainstRoot runs the ICS-23 membership check a bridge runs: the proof
// chains value → session IAVL root → app hash, and that app hash must be the
// root supplied by the trusted light client (7683_SURFACE §6.3 check 1).
func verifyAgainstRoot(root, key, value []byte, ops *cmtservice.ProofOps) error {
	proofOps := cmtprotocrypto.ProofOps{Ops: make([]cmtprotocrypto.ProofOp, len(ops.Ops))}
	for i, op := range ops.Ops {
		proofOps.Ops[i] = cmtprotocrypto.ProofOp{Type: op.Type, Key: op.Key, Data: op.Data}
	}
	merkleProof, err := commitmenttypes.ConvertProofs(&proofOps)
	if err != nil {
		return fmt.Errorf("convert proofs: %w", err)
	}
	return merkleProof.VerifyMembership(
		commitmenttypes.GetSDKSpecs(),
		commitmenttypes.NewMerkleRoot(root),
		commitmenttypes.NewMerklePath(sessiontypes.StoreKey, string(key)),
		value,
	)
}

// claimOnce models the settler-side first-claim ledger §6.3 check 4 mandates.
func claimOnce(ledger map[string]bool, receiptID string) error {
	if ledger[receiptID] {
		return fmt.Errorf("receipt %s was already claimed", short1(receiptID))
	}
	ledger[receiptID] = true
	return nil
}

func balanceOf(ctx context.Context, x *cli, addr, denom string) (math.Int, error) {
	res, err := banktypes.NewQueryClient(x.conn).Balance(ctx,
		&banktypes.QueryBalanceRequest{Address: addr, Denom: denom})
	if err != nil {
		return math.Int{}, err
	}
	return res.Balance.Amount, nil
}

// shortDenom abbreviates an ibc/<64 hex> voucher for log lines.
func shortDenom(d string) string {
	if strings.HasPrefix(d, "ibc/") && len(d) > 16 {
		return d[:16] + "…"
	}
	return d
}
