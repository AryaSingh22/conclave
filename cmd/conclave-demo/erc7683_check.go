// 7683-check — the live-node half of the task-2.4 surface contract
// (docs/edge/7683_SURFACE.md §7, D-086/D-087): a resolver-shaped sweep that
// exercises the fill-back evidence chain once against a running localnet —
// translated-fill submission through the mandate chokepoints, fallback
// clearing, and §6's evidence bundle verified exactly as an origin-chain
// bridge would (ICS-23 against the header-anchored app hash, terms_hash
// recomputed from the queried winning Solution, the leg check, dedup).
// The app-level conformance suite (app/erc7683_surface_conformance_test.go)
// proves the same claims through FinalizeBlock; this proves a real node
// serves them over real gRPC. Driven by scripts/7683_surface_check.sh.
// Dev tooling only — not consensus code.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"

	cmtservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	commitmenttypes "github.com/cosmos/ibc-go/v8/modules/core/23-commitment/types"

	"cosmossdk.io/math"

	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// erc7683Check runs the resolver-shaped live sweep. Actors: the resolver
// operator (sigil controller, Model A), its mandate-bound hot key, the
// order's recipient agent, and a rogue key.
func (c *cli) erc7683Check() error {
	ctx := context.Background()

	// ---- actors + funding (7683_SURFACE.md §1.2 custody split: the
	// OPERATOR holds the destination-liquidity float; the hot key holds
	// only gas + bond uclv) ----
	faucet, err := c.key("faucet")
	if err != nil {
		return err
	}
	op, err := c.ensureKey("7683-resolver-op")
	if err != nil {
		return err
	}
	hot, err := c.ensureKey("7683-resolver-hot")
	if err != nil {
		return err
	}
	user, err := c.ensureKey("7683-user")
	if err != nil {
		return err
	}
	rogue, err := c.ensureKey("7683-rogue")
	if err != nil {
		return err
	}
	for _, f := range []struct {
		to    sdk.AccAddress
		coins sdk.Coins
	}{
		{addrOf(op), sdk.NewCoins(sdk.NewInt64Coin(uclv, 100*oneCLV), sdk.NewInt64Coin(uusdc, 1_000*usdc))},
		{addrOf(hot), sdk.NewCoins(sdk.NewInt64Coin(uclv, 20*oneCLV))},
		{addrOf(user), sdk.NewCoins(sdk.NewInt64Coin(uclv, 20*oneCLV))},
		{addrOf(rogue), sdk.NewCoins(sdk.NewInt64Coin(uclv, 5*oneCLV))},
	} {
		if err := c.deliver(faucet, &banktypes.MsgSend{
			FromAddress: addrOf(faucet).String(), ToAddress: f.to.String(), Amount: f.coins,
		}); err != nil {
			return err
		}
	}

	opSigil, err := c.ensureSigil("7683-resolver-op", op)
	if err != nil {
		return err
	}
	userSigil, err := c.ensureSigil("7683-user", user)
	if err != nil {
		return err
	}
	fmt.Println("7683-check: actors funded, sigils registered")

	// ---- mandate: op → hot, submit/cancel only (§3 allowlist) ----
	height, err := c.height()
	if err != nil {
		return err
	}
	ceiling := sdk.NewCoins(sdk.NewInt64Coin(uusdc, 500*usdc), sdk.NewInt64Coin(uclv, 10*oneCLV))
	if err := c.deliver(op, &mandatetypes.MsgIssueMandate{
		Controller: addrOf(op).String(),
		Terms: mandatetypes.MandateTerms{
			SigilId:     opSigil,
			GranteeAddr: addrOf(hot).String(),
			AllowedMsgTypes: []string{
				sdk.MsgTypeURL(&veiltypes.MsgSubmitIntent{}),
				sdk.MsgTypeURL(&veiltypes.MsgCancelIntent{}),
			},
			SpendCeilingPerTx:  ceiling,
			SpendCeilingWindow: ceiling,
			WindowBlocks:       86_400,
			ExpiryHeight:       height + 7*86_400,
		},
	}); err != nil {
		return fmt.Errorf("issue resolver mandate: %w", err)
	}
	fmt.Println("7683-check: resolver mandate issued (submit/cancel; 500 USDC + bond budget) ✓")

	// ---- the translated fill + counterpart, one session (§3.1) ----
	height, err = c.height()
	if err != nil {
		return err
	}
	ttl := height + settleTTL
	fill := c.buildIntent(addrOf(hot), opSigil,
		[]veiltypes.IntentLeg{coinLeg(uusdc, 100*usdc)}, []veiltypes.IntentLeg{svcLeg(svcSchema, 10)}, ttl)
	counter := c.buildIntent(addrOf(user), userSigil,
		[]veiltypes.IntentLeg{svcLeg(svcSchema, 10)}, []veiltypes.IntentLeg{coinLeg(uusdc, 100*usdc)}, ttl)
	ids, err := c.submitConcurrent([]txSpec{{priv: hot, msg: fill}, {priv: user, msg: counter}})
	if err != nil {
		return fmt.Errorf("submit fill/counterpart: %w", err)
	}
	fillID := ids[0]
	if err := c.driveToState(faucet, ids, veiltypes.INTENT_STATE_CONCLUDED, 60); err != nil {
		return fmt.Errorf("the translated fill did not clear: %w", err)
	}
	fmt.Println("7683-check: translated fill CONCLUDED (fallback matcher; no solver) ✓")

	// a receipt written at height H is provable once header H+1 exists —
	// a tip ReceiptProof proves at latest−1 (7683_SURFACE.md §6.2). Nudge
	// one block past the settlement before gathering the evidence bundle,
	// or the proof comes back as a nonexistence proof for H−1.
	if err := c.deliver(faucet, &banktypes.MsgSend{
		FromAddress: addrOf(faucet).String(), ToAddress: addrOf(faucet).String(),
		Amount: sdk.NewCoins(sdk.NewInt64Coin(uclv, 1)),
	}); err != nil {
		return err
	}

	// ---- §6 evidence bundle, assembled the resolver-repo way ----
	rec, err := c.intentRecord(fillID)
	if err != nil {
		return err
	}
	sres, err := c.sessionc.Session(ctx, &sessiontypes.QuerySessionRequest{Id: rec.SessionId})
	if err != nil {
		return fmt.Errorf("session.Session(%d): %w", rec.SessionId, err)
	}
	// D-086 live proof: a FALLBACK-cleared session names its winning
	// solution (previously "" — the pre-image was unfetchable).
	if sres.Session.WinningSolutionId == "" {
		return fmt.Errorf("D-086: fallback session %d has empty winning_solution_id", rec.SessionId)
	}
	sols, err := c.sessionc.Solutions(ctx, &sessiontypes.QuerySolutionsRequest{SessionId: rec.SessionId})
	if err != nil {
		return fmt.Errorf("session.Solutions(%d): %w", rec.SessionId, err)
	}
	var matchBz []byte
	for i := range sols.Solutions {
		if sols.Solutions[i].Id != sres.Session.WinningSolutionId {
			continue
		}
		for j := range sols.Solutions[i].Matches {
			m := &sols.Solutions[i].Matches[j]
			for _, iid := range m.IntentIds {
				if iid == fillID {
					if matchBz, err = m.Marshal(); err != nil {
						return err
					}
				}
			}
		}
	}
	if len(matchBz) == 0 {
		return fmt.Errorf("winning match containing the fill not queryable (terms pre-image)")
	}

	recs, err := c.receiptc.Receipts(ctx, &receipttypes.QueryReceiptsRequest{SigilId: opSigil})
	if err != nil {
		return err
	}
	var receiptID string
	for _, r := range recs.Receipts {
		if r.IntentId == fillID {
			receiptID = r.Id
		}
	}
	if receiptID == "" {
		return fmt.Errorf("no receipt for fill %s", short1(fillID))
	}
	proof, err := c.receiptc.ReceiptProof(ctx, &receipttypes.QueryReceiptProofRequest{Id: receiptID})
	if err != nil {
		return fmt.Errorf("receipt.ReceiptProof: %w", err)
	}
	fmt.Printf("7683-check: evidence gathered (receipt %s, proof height %d, match pre-image %dB) ✓\n",
		short1(receiptID), proof.ProofHeight, len(matchBz))

	// ---- the §6.3 bridge checks, run exactly as an origin settler would ----
	// (1) root authenticity: the app hash AT ProofHeight is the AppHash of
	// the header at ProofHeight+1 (§6.2) — fetched here from the node the
	// way a light client would supply it.
	hdr, err := c.cmtc.GetBlockByHeight(ctx, &cmtservice.GetBlockByHeightRequest{Height: int64(proof.ProofHeight) + 1})
	if err != nil {
		return fmt.Errorf("header at ProofHeight+1: %w", err)
	}
	root := hdr.SdkBlock.Header.AppHash
	ridBz, err := sessiontypes.IDBytes(receiptID)
	if err != nil {
		return err
	}
	key := sessiontypes.ReceiptByIDKey(ridBz)
	value, err := proof.Receipt.Marshal()
	if err != nil {
		return err
	}
	var proofOps cmtprotocrypto.ProofOps
	if err := proofOps.Unmarshal(proof.Proof); err != nil {
		return err
	}
	merkleProof, err := commitmenttypes.ConvertProofs(&proofOps)
	if err != nil {
		return err
	}
	if err := merkleProof.VerifyMembership(
		commitmenttypes.GetSDKSpecs(),
		commitmenttypes.NewMerkleRoot(root),
		commitmenttypes.NewMerklePath(sessiontypes.StoreKey, string(key)),
		value,
	); err != nil {
		return fmt.Errorf("§6.3(1) ICS-23 against header-anchored app hash: %w", err)
	}
	// (2) terms authenticity.
	sum := sha256.Sum256(matchBz)
	if !bytes.Equal(sum[:], proof.Receipt.TermsHash) {
		return fmt.Errorf("§6.3(2) terms_hash mismatch: pre-image does not recompute")
	}
	var match sessiontypes.Match
	if err := match.Unmarshal(matchBz); err != nil {
		return err
	}
	// (3) the leg check: order = {recipient userSigil, 100 uusdc, unconditioned}.
	legOK := false
	for i := range match.Legs {
		leg := &match.Legs[i]
		coin := leg.GetCoin()
		if leg.ToSigil == userSigil && coin != nil && coin.Denom == uusdc &&
			coin.Amount.GTE(math.NewInt(100*usdc)) && leg.Condition == nil {
			legOK = true
		}
	}
	if !legOK {
		return fmt.Errorf("§6.3(3) no unconditioned leg pays the order recipient")
	}
	// (3-negative) a mismatched order MUST fail the same check.
	wrongOK := false
	for i := range match.Legs {
		leg := &match.Legs[i]
		coin := leg.GetCoin()
		if leg.ToSigil == opSigil && coin != nil && coin.Denom == uusdc &&
			coin.Amount.GTE(math.NewInt(500*usdc)) {
			wrongOK = true
		}
	}
	if wrongOK {
		return fmt.Errorf("§6.3(3) mismatched order unexpectedly satisfied")
	}
	// (4) dedup: the first claim consumes the receipt id; presenting the
	// same (still-valid) evidence again is refused by the mark alone.
	claimed := map[string]bool{}
	claim := func() bool {
		if claimed[proof.Receipt.Id] {
			return false
		}
		claimed[proof.Receipt.Id] = true
		return true
	}
	if !claim() {
		return fmt.Errorf("§6.3(4) first claim must succeed")
	}
	if claim() {
		return fmt.Errorf("§6.3(4) replayed claim must be refused by dedup")
	}
	fmt.Println("7683-check: §6.3 checks 1–4 verified live (root, terms, leg, dedup) ✓")

	// ---- live negatives ----
	// under-declared fill (§3.1): give 50 uusdc, declare 1 — passes the ante
	// (D-065 uniform valuation), INVALID at the Reveal, never settles.
	height, err = c.height()
	if err != nil {
		return err
	}
	under := c.buildIntent(addrOf(hot), opSigil,
		[]veiltypes.IntentLeg{coinLeg(uusdc, 50*usdc)}, []veiltypes.IntentLeg{svcLeg(svcSchema, 5)}, height+settleTTL)
	underDeclare(under, sdk.NewCoins(sdk.NewInt64Coin(uusdc, 1*usdc)))
	uh, err := c.deliverFee(hot, sdk.Coins{}, under)
	if err != nil {
		return fmt.Errorf("under-declared submit must PASS the ante: %w", err)
	}
	underID, err := c.intentIDForTx(uh)
	if err != nil {
		return err
	}
	if err := c.driveToState(faucet, []string{underID}, veiltypes.INTENT_STATE_INVALID, 20); err != nil {
		return fmt.Errorf("under-declared fill must go INVALID at the Reveal: %w", err)
	}
	fmt.Println("7683-check: under-declared fill passed ante, INVALID at Reveal (never settles) ✓")

	// over-ceiling → ErrMandateExceeded in CheckTx.
	over := c.buildIntent(addrOf(hot), opSigil,
		[]veiltypes.IntentLeg{coinLeg(uusdc, 600*usdc)}, []veiltypes.IntentLeg{svcLeg(svcSchema, 1)}, height+settleTTL)
	ccode, _, err := c.broadcastCheck(hot, over)
	if err != nil {
		return err
	}
	if ccode != mandatetypes.ErrMandateExceeded.ABCICode() {
		return fmt.Errorf("over-ceiling fill: CheckTx code %d, want ErrMandateExceeded", ccode)
	}
	// foreign sigil / unbound key → ErrSigilMismatch (ante prevalidation, D-085).
	foreign := c.buildIntent(addrOf(hot), userSigil, nil,
		[]veiltypes.IntentLeg{svcLeg(svcSchema, 1)}, height+settleTTL)
	fcode, _, err := c.broadcastCheck(hot, foreign)
	if err != nil {
		return err
	}
	if fcode != veiltypes.ErrSigilMismatch.ABCICode() {
		return fmt.Errorf("foreign-sigil fill: CheckTx code %d, want ErrSigilMismatch", fcode)
	}
	// an UNBOUND key skips the D-085 ante prevalidation (correct scope —
	// no mandate, nothing to strand), so its rejection is the msg server's,
	// in DeliverTx.
	rmsg := c.buildIntent(addrOf(rogue), opSigil, nil,
		[]veiltypes.IntentLeg{svcLeg(svcSchema, 1)}, height+settleTTL)
	rcode, rlog, err := c.deliverCode(rogue, sdk.Coins{}, rmsg)
	if err != nil {
		return err
	}
	if rcode != veiltypes.ErrSigilMismatch.ABCICode() {
		return fmt.Errorf("unbound-key fill: DeliverTx code %d (%s), want ErrSigilMismatch", rcode, firstLine(rlog))
	}
	fmt.Println("7683-check: live negatives — under-declared/over-ceiling/foreign-sigil/unbound-key ✓")

	fmt.Println("7683 SURFACE CHECK OK")
	return nil
}

// underDeclare rewrites a built submit msg's envelope to declare the given
// spend_commitment instead of the honest give-derived one — the §3.1 D-065
// negative's lever.
func underDeclare(msg *veiltypes.MsgSubmitIntent, commitment sdk.Coins) {
	var env veiltypes.IntentEnvelope
	if err := env.Unmarshal(msg.Envelope); err != nil {
		panic(err)
	}
	env.SpendCommitment = commitment
	bz, err := env.Marshal()
	if err != nil {
		panic(err)
	}
	msg.Envelope = bz
}
