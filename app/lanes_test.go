package app

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"

	unorderedtypes "github.com/AryaSingh22/conclave/app/unordered/types"
	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

func laneTestTxConfig(t *testing.T) client.TxConfig {
	t.Helper()
	encCfg := moduletestutil.MakeTestEncodingConfig()
	unorderedtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	return encCfg.TxConfig
}

func laneTestAddr(seed string) string {
	hash := sha256.Sum256([]byte(seed))
	return sdk.AccAddress(hash[:20]).String()
}

// buildLaneTx builds an unsigned tx; lane predicates and priorities are
// pure functions of the msgs, so no signatures are required.
func buildLaneTx(t *testing.T, txConfig client.TxConfig, msgs ...sdk.Msg) sdk.Tx {
	t.Helper()
	b := txConfig.NewTxBuilder()
	require.NoError(t, b.SetMsgs(msgs...))
	return b.GetTx()
}

// buildLaneSignedTx attaches a pubkey + claimed sequence (no real
// signature; the signer extractor only reads signer data) and, if ext is
// non-nil, the TxBody extension option.
func buildLaneSignedTx(
	t *testing.T, txConfig client.TxConfig, priv cryptotypes.PrivKey,
	seq uint64, ext *codectypes.Any, msgs ...sdk.Msg,
) sdk.Tx {
	t.Helper()
	b := txConfig.NewTxBuilder()
	require.NoError(t, b.SetMsgs(msgs...))
	b.SetGasLimit(200_000)
	if ext != nil {
		extBuilder, ok := b.(interface{ SetExtensionOptions(...*codectypes.Any) })
		require.True(t, ok, "tx builder must support extension options")
		extBuilder.SetExtensionOptions(ext)
	}
	require.NoError(t, b.SetSignatures(signingtypes.SignatureV2{
		PubKey:   priv.PubKey(),
		Data:     &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
		Sequence: seq,
	}))
	return b.GetTx()
}

func unorderedExt(t *testing.T, timeout time.Time) *codectypes.Any {
	t.Helper()
	ext, err := codectypes.NewAnyWithValue(&unorderedtypes.UnorderedTxExtension{TimeoutTimestamp: timeout})
	require.NoError(t, err)
	return ext
}

func veilSubmitMsg(t *testing.T, feeBid string) *veiltypes.MsgSubmitIntent {
	t.Helper()
	env := veiltypes.IntentEnvelope{TtlHeight: 100, FeeBid: feeBid}
	bz, err := env.Marshal()
	require.NoError(t, err)
	return &veiltypes.MsgSubmitIntent{
		Sender:      laneTestAddr("alice"),
		SenderSigil: "sigil-alice",
		Ciphertext:  []byte{0x01},
		Envelope:    bz,
		Bond:        sdk.NewInt64Coin("uclv", 1_000_000),
	}
}

func veilCancelMsg() *veiltypes.MsgCancelIntent {
	return &veiltypes.MsgCancelIntent{Sender: laneTestAddr("alice"), IntentId: "aa"}
}

func bankSendMsg() *banktypes.MsgSend {
	return &banktypes.MsgSend{
		FromAddress: laneTestAddr("alice"),
		ToAddress:   laneTestAddr("bob"),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("uusdc", 1)),
	}
}

func ibcTransferMsg() *ibctransfertypes.MsgTransfer {
	return ibctransfertypes.NewMsgTransfer(
		"transfer", "channel-0", sdk.NewInt64Coin("uclv", 1),
		laneTestAddr("alice"), laneTestAddr("bob"),
		clienttypes.NewHeight(1, 1000), 0, "",
	)
}

func mustLanes(t *testing.T, txConfig client.TxConfig) []laneNamed {
	t.Helper()
	lanes, err := NewLanes(log.NewNopLogger(), txConfig, nil)
	require.NoError(t, err)
	named := make([]laneNamed, len(lanes))
	for i, l := range lanes {
		named[i] = laneNamed{name: l.Name(), match: l.Match, space: l.GetMaxBlockSpace()}
	}
	return named
}

type laneNamed struct {
	name  string
	match func(sdk.Context, sdk.Tx) bool
	space math.LegacyDec
}

// TestLaneRegistryOrderAndShares pins the SPEC §6.7 lane order and max
// block space shares as produced by the production constructor.
func TestLaneRegistryOrderAndShares(t *testing.T) {
	lanes := mustLanes(t, laneTestTxConfig(t))

	names := make([]string, len(lanes))
	total := math.LegacyZeroDec()
	for i, l := range lanes {
		names[i] = l.name
		total = total.Add(l.space)
	}
	require.Equal(t, []string{LaneVeil, LaneSession, LaneCommerce, LaneIBC, LaneGeneral}, names)

	require.Equal(t, math.LegacyMustNewDecFromStr("0.30"), lanes[0].space)
	require.Equal(t, math.LegacyMustNewDecFromStr("0.25"), lanes[1].space)
	require.Equal(t, math.LegacyMustNewDecFromStr("0.15"), lanes[2].space)
	require.Equal(t, math.LegacyMustNewDecFromStr("0.15"), lanes[3].space)
	require.Equal(t, math.LegacyMustNewDecFromStr("0.15"), lanes[4].space)
	require.Equal(t, math.LegacyOneDec(), total, "shares must sum to exactly 1")
}

// laneRoutingCases is the lane-membership truth table (SPEC §6.7): each
// msg class alone, plus every mixing combination routing to the
// lowest-priority matching lane.
func laneRoutingCases(t *testing.T) map[string][]sdk.Msg {
	t.Helper()
	return map[string][]sdk.Msg{
		// pure classes
		"veil submit":         {veilSubmitMsg(t, "1")},
		"veil cancel":         {veilCancelMsg()},
		"veil submit+cancel":  {veilSubmitMsg(t, "1"), veilCancelMsg()},
		"session register":    {&sessiontypes.MsgRegisterSolver{}},
		"session unbond":      {&sessiontypes.MsgUnbondSolver{}},
		"session propose":     {&sessiontypes.MsgProposeSolution{}},
		"session conclude":    {&sessiontypes.MsgConclude{}},
		"commerce sigil":      {&sigiltypes.MsgRegisterSigil{}},
		"commerce rotate":     {&sigiltypes.MsgRotateController{}},
		"commerce anchor":     {&sigiltypes.MsgAnchorCredential{}},
		"commerce revokecred": {&sigiltypes.MsgRevokeCredential{}},
		"commerce issue":      {&mandatetypes.MsgIssueMandate{}},
		"commerce revoke":     {&mandatetypes.MsgRevokeMandate{}},
		"commerce amend":      {&mandatetypes.MsgAmendMandate{}},
		"commerce release":    {&escrowtypes.MsgReleaseEscrow{}},
		"commerce dispute":    {&escrowtypes.MsgRaiseDispute{}},
		"commerce proof":      {&escrowtypes.MsgSubmitDeliveryProof{}},
		"ibc transfer":        {ibcTransferMsg()},
		"general send":        {bankSendMsg()},

		// MsgUpdateParams are NOT lane-class msgs: veil is
		// "MsgSubmitIntent/MsgCancelIntent only" (SPEC §6.7) and the same
		// reading applies to every module's params msg.
		"veil params → general":    {&veiltypes.MsgUpdateParams{}},
		"session params → general": {&sessiontypes.MsgUpdateParams{}},

		// mixes route to the LOWEST-priority matching lane (SPEC §6.7)
		"veil+session → session":      {veilSubmitMsg(t, "1"), &sessiontypes.MsgProposeSolution{}},
		"veil+commerce → commerce":    {veilSubmitMsg(t, "1"), &sigiltypes.MsgRegisterSigil{}},
		"session+commerce → commerce": {&sessiontypes.MsgConclude{}, &mandatetypes.MsgIssueMandate{}},
		"veil+ibc → ibc":              {veilSubmitMsg(t, "1"), ibcTransferMsg()},
		"all four → ibc":              {veilSubmitMsg(t, "1"), &sessiontypes.MsgConclude{}, &sigiltypes.MsgRegisterSigil{}, ibcTransferMsg()},
		"veil+general → general":      {veilSubmitMsg(t, "1"), bankSendMsg()},
		"everything+send → general":   {veilSubmitMsg(t, "1"), &sessiontypes.MsgConclude{}, &sigiltypes.MsgRegisterSigil{}, ibcTransferMsg(), bankSendMsg()},
		"no msgs → general":           {},
	}
}

func expectedLaneFor(name string) string {
	switch {
	case strings.Contains(name, "→ session"):
		return LaneSession
	case strings.Contains(name, "→ commerce"):
		return LaneCommerce
	case strings.Contains(name, "→ ibc"):
		return LaneIBC
	case strings.Contains(name, "→ general"):
		return LaneGeneral
	case strings.HasPrefix(name, "veil"):
		return LaneVeil
	case strings.HasPrefix(name, "session"):
		return LaneSession
	case strings.HasPrefix(name, "commerce"):
		return LaneCommerce
	case strings.HasPrefix(name, "ibc"):
		return LaneIBC
	default:
		return LaneGeneral
	}
}

// TestLaneMatchExclusivity proves the predicates partition the tx
// universe: every tx matches EXACTLY one lane, and it is the designated
// one. This is what makes the lowest-priority-matching-lane rule hold
// independent of lane registration order.
func TestLaneMatchExclusivity(t *testing.T) {
	txConfig := laneTestTxConfig(t)
	lanes := mustLanes(t, txConfig)
	ctx := sdk.Context{}

	for name, msgs := range laneRoutingCases(t) {
		tx := buildLaneTx(t, txConfig, msgs...)
		var matched []string
		for _, lane := range lanes {
			if lane.match(ctx, tx) {
				matched = append(matched, lane.name)
			}
		}
		require.Equal(t, []string{expectedLaneFor(name)}, matched,
			"case %q must match exactly its designated lane", name)
	}
}

// TestLaneRegistrationOrderGuard runs Match against the FULL registered
// lane list in registration order and takes the FIRST match — the exact
// insertion rule of the laned mempool. A veil-class tx must land in veil,
// never a match-anything lane later in the registry; this guards the
// routing against lane-registration reordering, not just per-predicate
// behavior in isolation.
func TestLaneRegistrationOrderGuard(t *testing.T) {
	txConfig := laneTestTxConfig(t)
	lanes := mustLanes(t, txConfig)
	ctx := sdk.Context{}

	for name, msgs := range laneRoutingCases(t) {
		tx := buildLaneTx(t, txConfig, msgs...)
		first := ""
		for _, lane := range lanes {
			if lane.match(ctx, tx) {
				first = lane.name
				break
			}
		}
		require.Equal(t, expectedLaneFor(name), first,
			"case %q: first matching lane in registration order", name)
	}
}

// TestVeilPriorityKey pins the fee_bid ordering semantics (SPEC §6.3,
// D-035): numeric order via fixed-width keys, max across msgs, zero for
// cancels and malformed/negative bids, clamp for absurd bids — never an
// error, never a panic.
func TestVeilPriorityKey(t *testing.T) {
	txConfig := laneTestTxConfig(t)
	prio := VeilTxPriority()

	key := func(msgs ...sdk.Msg) string {
		return veilPriorityKey(buildLaneTx(t, txConfig, msgs...))
	}

	k10 := key(veilSubmitMsg(t, "10"))
	k5 := key(veilSubmitMsg(t, "5"))
	k15 := key(veilSubmitMsg(t, "1.5"))
	kZero := key(veilSubmitMsg(t, "0"))

	require.Equal(t, 1, prio.Compare(k10, k5), "10 > 5")
	require.Equal(t, 1, prio.Compare(k5, k15), "5 > 1.5")
	require.Equal(t, 1, prio.Compare(k15, kZero), "1.5 > 0")
	require.Equal(t, 0, prio.Compare(k10, key(veilSubmitMsg(t, "10"))), "equal bids tie")

	// cancels carry no envelope → zero
	require.Equal(t, kZero, key(veilCancelMsg()))
	// malformed envelope bytes → zero, no error
	broken := veilSubmitMsg(t, "10")
	broken.Envelope = []byte{0xFF, 0xFF, 0xFF}
	require.Equal(t, kZero, key(broken))
	// unparseable / negative bids → zero
	require.Equal(t, kZero, key(veilSubmitMsg(t, "not-a-number")))
	require.Equal(t, kZero, key(veilSubmitMsg(t, "-3")))
	// empty bid → zero
	require.Equal(t, kZero, key(veilSubmitMsg(t, "")))
	// max across a multi-msg tx
	require.Equal(t, key(veilSubmitMsg(t, "7")), key(veilSubmitMsg(t, "2"), veilSubmitMsg(t, "7")))
	// absurd bids clamp to the maximum key instead of erroring
	require.Equal(t, maxVeilPriorityKey, key(veilSubmitMsg(t, strings.Repeat("9", 45))))
	// MinValue sorts below everything, including zero
	require.Equal(t, -1, prio.Compare(prio.MinValue, kZero))
}

// TestUnorderedNonceAdapter pins D-034: ordered txs keep their claimed
// sequence; unordered txs report a synthetic hash-derived nonce, so two
// unordered txs claiming the same sequence stay distinct in the lane
// mempool's (sender, nonce) index.
func TestUnorderedNonceAdapter(t *testing.T) {
	txConfig := laneTestTxConfig(t)
	adapter := NewUnorderedNonceAdapter(txConfig.TxEncoder())
	priv := secp256k1.GenPrivKey()
	timeout := time.Unix(1_750_000_500, 0).UTC()

	// ordered: claimed sequence preserved
	ordered := buildLaneSignedTx(t, txConfig, priv, 42, nil, bankSendMsg())
	signers, err := adapter.GetSigners(ordered)
	require.NoError(t, err)
	require.Len(t, signers, 1)
	require.Equal(t, uint64(42), signers[0].Sequence)

	// unordered: synthetic nonce = BE uint64 of sha256(txBytes)[:8]
	unordered := buildLaneSignedTx(t, txConfig, priv, 7, unorderedExt(t, timeout), bankSendMsg())
	bz, err := txConfig.TxEncoder()(unordered)
	require.NoError(t, err)
	hash := sha256.Sum256(bz)
	want := binary.BigEndian.Uint64(hash[:8])

	signers, err = adapter.GetSigners(unordered)
	require.NoError(t, err)
	require.Equal(t, want, signers[0].Sequence)

	// deterministic across calls
	again, err := adapter.GetSigners(unordered)
	require.NoError(t, err)
	require.Equal(t, signers[0].Sequence, again[0].Sequence)

	// two unordered txs with the SAME claimed sequence → distinct nonces
	other := buildLaneSignedTx(t, txConfig, priv, 7, unorderedExt(t, timeout),
		&banktypes.MsgSend{
			FromAddress: laneTestAddr("alice"),
			ToAddress:   laneTestAddr("carol"),
			Amount:      sdk.NewCoins(sdk.NewInt64Coin("uusdc", 2)),
		})
	otherSigners, err := adapter.GetSigners(other)
	require.NoError(t, err)
	require.NotEqual(t, signers[0].Sequence, otherSigners[0].Sequence,
		"distinct unordered txs must never collide on (sender, nonce)")

	// a present-but-malformed extension is a hard error (mirrors D-026)
	malformed := buildLaneSignedTx(t, txConfig, priv, 7,
		&codectypes.Any{TypeUrl: unorderedtypes.ExtensionTypeURL, Value: []byte{0xFF, 0xFF}},
		bankSendMsg())
	_, err = adapter.GetSigners(malformed)
	require.Error(t, err, "malformed unordered extension must never silently degrade to ordered")
}
