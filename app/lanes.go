// Lane definitions and block space shares (SPEC §6.7, §4 "app/lanes.go").
//
// Five lanes in priority order with their max block space:
//
//	veil 30% → session 25% → commerce 15% → ibc 15% → general 15%
//
// Lane membership is decided by a match(tx) predicate over the tx's msg
// TypeURLs. A tx mixing lanes routes to the LOWEST-priority matching lane
// (SPEC §6.7) — implemented by classifying every msg and routing the tx to
// the lane of its lowest-priority msg class. The predicates therefore
// partition the tx universe: every tx matches exactly one lane, regardless
// of lane registration order. Client-dev documentation: docs/LANES.md.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"strings"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"

	signerextraction "github.com/skip-mev/block-sdk/v2/adapters/signer_extraction_adapter"
	"github.com/skip-mev/block-sdk/v2/block"
	blockbase "github.com/skip-mev/block-sdk/v2/block/base"

	unorderedtypes "github.com/AryaSingh22/conclave/app/unordered/types"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// Lane names (SPEC §6.7 order; index = priority, 0 highest).
const (
	LaneVeil     = "veil"
	LaneSession  = "session"
	LaneCommerce = "commerce"
	LaneIBC      = "ibc"
	LaneGeneral  = "general"
)

// laneClass is a msg/tx lane classification. Numeric value ascends as lane
// priority DESCENDS, so the lowest-priority class of a tx is the numeric
// maximum over its msgs.
type laneClass int

const (
	classVeil laneClass = iota
	classSession
	classCommerce
	classIBC
	classGeneral
)

// ibcMsgPrefix classifies every ibc-go msg (core client/connection/channel
// and apps such as transfer) without enumerating TypeURLs.
const ibcMsgPrefix = "/ibc."

// msgClasses maps the conclave msg TypeURLs to their lane class. Msgs not
// listed here (including every MsgUpdateParams, gov, bank, authz, …) are
// general-class: the veil lane is "MsgSubmitIntent/MsgCancelIntent only"
// (SPEC §6.7) and the same only-the-named-msgs reading applies to session
// and commerce. The map is read-only after init (never iterated into state).
var msgClasses = map[string]laneClass{
	// veil: MsgSubmitIntent / MsgCancelIntent only (SPEC §6.7)
	sdk.MsgTypeURL(&veiltypes.MsgSubmitIntent{}): classVeil,
	sdk.MsgTypeURL(&veiltypes.MsgCancelIntent{}): classVeil,

	// session: solver + conclude msgs (SPEC §6.7)
	sdk.MsgTypeURL(&sessiontypes.MsgRegisterSolver{}):  classSession,
	sdk.MsgTypeURL(&sessiontypes.MsgUnbondSolver{}):    classSession,
	sdk.MsgTypeURL(&sessiontypes.MsgProposeSolution{}): classSession,
	sdk.MsgTypeURL(&sessiontypes.MsgConclude{}):        classSession,

	// commerce: sigil / mandate / escrow msgs (SPEC §6.7)
	sdk.MsgTypeURL(&sigiltypes.MsgRegisterSigil{}):        classCommerce,
	sdk.MsgTypeURL(&sigiltypes.MsgRotateController{}):     classCommerce,
	sdk.MsgTypeURL(&sigiltypes.MsgAnchorCredential{}):     classCommerce,
	sdk.MsgTypeURL(&sigiltypes.MsgRevokeCredential{}):     classCommerce,
	sdk.MsgTypeURL(&mandatetypes.MsgIssueMandate{}):       classCommerce,
	sdk.MsgTypeURL(&mandatetypes.MsgRevokeMandate{}):      classCommerce,
	sdk.MsgTypeURL(&mandatetypes.MsgAmendMandate{}):       classCommerce,
	sdk.MsgTypeURL(&escrowtypes.MsgReleaseEscrow{}):       classCommerce,
	sdk.MsgTypeURL(&escrowtypes.MsgRaiseDispute{}):        classCommerce,
	sdk.MsgTypeURL(&escrowtypes.MsgSubmitDeliveryProof{}): classCommerce,
}

// classOfMsg classifies a single msg by its top-level TypeURL. Wrapper msgs
// (authz MsgExec, gov proposals) classify as the wrapper, never their
// contents (D-033): routing must be a pure function of the outer TypeURL.
func classOfMsg(msg sdk.Msg) laneClass {
	url := sdk.MsgTypeURL(msg)
	if class, ok := msgClasses[url]; ok {
		return class
	}
	if strings.HasPrefix(url, ibcMsgPrefix) {
		return classIBC
	}
	return classGeneral
}

// classOfTx routes a tx to the lane of its lowest-priority msg class —
// the SPEC §6.7 "lowest-priority matching lane" rule for mixed txs. A tx
// with no msgs is general-class (it can never execute anyway).
func classOfTx(tx sdk.Tx) laneClass {
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return classGeneral
	}
	lowest := classVeil
	for _, msg := range msgs {
		if class := classOfMsg(msg); class > lowest {
			lowest = class
		}
	}
	return lowest
}

// matchClass builds the lane's match(tx) predicate (SPEC §6.7). The five
// predicates are mutually exclusive and total over the tx universe.
func matchClass(class laneClass) blockbase.MatchHandler {
	return func(_ sdk.Context, tx sdk.Tx) bool {
		return classOfTx(tx) == class
	}
}

// ---- unordered-tx signer extraction (D-034) ----

// unorderedNonceAdapter wraps the default Block SDK signer extractor with
// one delta: txs carrying the UnorderedTxExtension (D-026) report a
// synthetic nonce derived from the tx hash instead of their claimed
// sequence. The lane mempool indexes and replaces txs by (sender, nonce),
// so two unordered txs claiming the same sequence would otherwise silently
// replace each other on Insert — and a proposal carrying both would be
// REJECTED by the lane comparator's same-sequence error. Replay protection
// never derives from this value; it remains the dedup cache (D-028).
type unorderedNonceAdapter struct {
	txEncoder sdk.TxEncoder
	fallback  signerextraction.Adapter
}

// NewUnorderedNonceAdapter returns the lane signer extractor used by every
// Conclave lane (SPEC §6.7 unordered txs × lanes; D-034).
func NewUnorderedNonceAdapter(txEncoder sdk.TxEncoder) signerextraction.Adapter {
	return unorderedNonceAdapter{
		txEncoder: txEncoder,
		fallback:  signerextraction.NewDefaultAdapter(),
	}
}

// GetSigners implements signerextraction.Adapter.
func (a unorderedNonceAdapter) GetSigners(tx sdk.Tx) ([]signerextraction.SignerData, error) {
	signers, err := a.fallback.GetSigners(tx)
	if err != nil {
		return nil, err
	}

	// A present-but-malformed extension is a hard error, mirroring the
	// ante handler (D-026): it must never silently degrade to ordered.
	_, unordered, err := unorderedtypes.TimeoutFromTx(tx)
	if err != nil {
		return nil, err
	}
	if !unordered {
		return signers, nil
	}

	bz, err := a.txEncoder(tx)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(bz)
	nonce := binary.BigEndian.Uint64(hash[:8])
	for i := range signers {
		signers[i].Sequence = nonce
	}
	return signers, nil
}

// ---- veil lane ordering (SPEC §6.3) ----

// veilPriorityWidth is the fixed width of the zero-padded priority key.
// LegacyDec values are scaled integers (18 fractional digits); 60 digits
// bounds fee bids below 10^42 uclv — unreachable, and larger bids clamp
// to the maximum key rather than erroring (ordering, not validation).
const veilPriorityWidth = 60

// maxVeilPriorityKey is the clamp for absurdly large fee bids.
var maxVeilPriorityKey = strings.Repeat("9", veilPriorityWidth)

// VeilTxPriority orders veil-lane txs by envelope fee_bid (SPEC §6.3
// "ordered by fee_bid then submit order"). The priority is a fixed-width
// decimal string so lexicographic comparison equals numeric comparison —
// a pure function of the tx bytes, verifiable by every node in
// ProcessProposal. Equal fee_bids fall back to the priority-nonce index's
// deterministic (sender, sequence) tie-break: literal proposer-local
// submit order is unverifiable in consensus and irrelevant under
// uniform-price batch clearing (D-035; docs/LANES.md trust model).
func VeilTxPriority() blockbase.TxPriority[string] {
	return blockbase.TxPriority[string]{
		GetTxPriority: func(_ context.Context, tx sdk.Tx) string {
			return veilPriorityKey(tx)
		},
		Compare: func(a, b string) int {
			return strings.Compare(a, b)
		},
		MinValue: "",
	}
}

// veilPriorityKey returns the fixed-width key for the tx's best fee_bid.
// The bid is the max across the tx's MsgSubmitIntent envelopes;
// MsgCancelIntent carries no envelope and contributes zero. Unparseable,
// empty, or negative fee_bids are zero — ordering metadata must never
// reject or panic (SPEC §11.6); envelope validation proper is the msg
// server's job (task 0.7).
func veilPriorityKey(tx sdk.Tx) string {
	best := math.LegacyZeroDec()
	for _, msg := range tx.GetMsgs() {
		submit, ok := msg.(*veiltypes.MsgSubmitIntent)
		if !ok {
			continue
		}
		var env veiltypes.IntentEnvelope
		if err := env.Unmarshal(submit.Envelope); err != nil {
			continue
		}
		bid, err := math.LegacyNewDecFromStr(env.FeeBid)
		if err != nil || bid.IsNegative() {
			continue
		}
		if bid.GT(best) {
			best = bid
		}
	}

	// LegacyDec.BigInt() is the value scaled by 10^18 — an integer whose
	// natural order equals the Dec order.
	scaled := best.BigInt().String()
	if len(scaled) > veilPriorityWidth {
		return maxVeilPriorityKey
	}
	return strings.Repeat("0", veilPriorityWidth-len(scaled)) + scaled
}

// ---- lane construction ----

// laneSpaces pins the SPEC §6.7 max block space per lane, in priority
// order. The shares sum to exactly 1; the laned mempool rejects any other
// total at construction.
var laneSpaces = []struct {
	name  string
	class laneClass
	space math.LegacyDec
}{
	{LaneVeil, classVeil, math.LegacyMustNewDecFromStr("0.30")},
	{LaneSession, classSession, math.LegacyMustNewDecFromStr("0.25")},
	{LaneCommerce, classCommerce, math.LegacyMustNewDecFromStr("0.15")},
	{LaneIBC, classIBC, math.LegacyMustNewDecFromStr("0.15")},
	{LaneGeneral, classGeneral, math.LegacyMustNewDecFromStr("0.15")},
}

// laneAnteHandler restores baseapp's ante context contract for lane-driven
// verification: the Block SDK invokes the ante chain directly during
// PrepareProposal/ProcessProposal without setting ctx.TxBytes (baseapp's
// runTx always does), which would give every unordered tx in a proposal
// the hash of EMPTY bytes — the dedup cache would then reject all but the
// first as replays (D-034). The tx re-encodes to its original bytes, so
// the injected hash input equals what CheckTx and FinalizeBlock hash.
func laneAnteHandler(txEncoder sdk.TxEncoder, anteHandler sdk.AnteHandler) sdk.AnteHandler {
	if anteHandler == nil {
		return nil
	}
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if len(ctx.TxBytes()) == 0 {
			bz, err := txEncoder(tx)
			if err != nil {
				return ctx, err
			}
			ctx = ctx.WithTxBytes(bz)
		}
		return anteHandler(ctx, tx, simulate)
	}
}

// NewLanes builds the five SPEC §6.7 lanes in priority order. Every lane
// verifies txs with the production ante handler, so proposal-time
// verification (PrepareProposal AND ProcessProposal) enforces exactly the
// CheckTx rules — mandate window charging included, accumulated across the
// whole proposal via cascading cache writes (SPEC §6.2 step 5).
func NewLanes(logger log.Logger, txConfig client.TxConfig, anteHandler sdk.AnteHandler) ([]block.Lane, error) {
	extractor := NewUnorderedNonceAdapter(txConfig.TxEncoder())
	laneAnte := laneAnteHandler(txConfig.TxEncoder(), anteHandler)

	lanes := make([]block.Lane, 0, len(laneSpaces))
	for _, spec := range laneSpaces {
		cfg := blockbase.NewLaneConfig(
			logger,
			txConfig.TxEncoder(),
			txConfig.TxDecoder(),
			laneAnte,
			extractor,
			spec.space,
		)

		options := []blockbase.LaneOption{
			blockbase.WithMatchHandler(matchClass(spec.class)),
		}
		if spec.class == classVeil {
			// veil lane orders by envelope fee_bid (SPEC §6.3)
			options = append(options, blockbase.WithMempoolConfigs(cfg, VeilTxPriority()))
		}

		lane, err := blockbase.NewBaseLane(cfg, spec.name, options...)
		if err != nil {
			return nil, err
		}
		lanes = append(lanes, lane)
	}

	return lanes, nil
}
