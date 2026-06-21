package app

import (
	"bytes"
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/core/comet"
	"cosmossdk.io/core/header"
	"cosmossdk.io/log"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtsecp256k1 "github.com/cometbft/cometbft/crypto/secp256k1"
	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	protoio "github.com/cosmos/gogoproto/io"
	"github.com/cosmos/gogoproto/proto"

	"github.com/AryaSingh22/conclave/crypto/threshold"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

const veTestChainID = "conclave-dev-1"

// TestRevealTransportCollectAndDecrypt is the core STOP #2 transport proof: the
// vote-extension data path delivers VALID, SUFFICIENT reveal shares that decrypt
// a real threshold ciphertext — and the F-4 guarantee that FEWER than t shares
// (the pre-ordering / partial-quorum state) CANNOT decrypt. It runs a real DKG,
// encrypts to the epoch key, has each node PartialReveal (exactly as ExtendVote
// does), packs the shares into vote extensions, and runs the real collect() the
// PreBlocker uses — then feeds the collected shares to the real reveal scheme.
func TestRevealTransportCollectAndDecrypt(t *testing.T) {
	const n, thr = 4, 3
	mats, err := threshold.RunDKGInProcess(n, thr)
	require.NoError(t, err)

	scheme, err := threshold.NewScheme(mats[0].Commits, n) // reveal-capable (public commits)
	require.NoError(t, err)
	client := threshold.NewClientScheme()

	plaintext := []byte("sealed-intent-payload-v1")
	ct, err := client.Encrypt(mats[0].EpochPub, plaintext)
	require.NoError(t, err)

	const intentID = "deadbeef"

	// F-4 (no shares = no decrypt): a ciphertext whose ordering is not yet
	// reflected in any vote extension cannot be combined.
	_, err = scheme.Combine(nil, ct)
	require.ErrorIs(t, err, threshold.ErrInsufficientShares, "zero shares must not decrypt")

	// build each node's vote extension (one share for the in-flight ciphertext),
	// exactly as ExtendVoteHandler does: PartialReveal with the node-local share.
	votes := make([]abci.ExtendedVoteInfo, n)
	for i := 0; i < n; i++ {
		share, err := client.PartialReveal(mats[i].Share, ct)
		require.NoError(t, err)
		payload := veiltypes.VoteExtensionShares{Shares: []veiltypes.PartialRevealShare{
			{IntentId: intentID, DkgIndex: uint32(mats[i].Index), Share: share},
		}}
		bz, err := payload.Marshal()
		require.NoError(t, err)
		votes[i] = abci.ExtendedVoteInfo{BlockIdFlag: cmtproto.BlockIDFlagCommit, VoteExtension: bz}
	}

	h := &RevealVoteExtensions{cache: NewRevealShareCache(), logger: log.NewNopLogger()}

	// F-4 (t-1 shares still cannot decrypt): collect a sub-threshold subset.
	injSub, err := marshalInjected(abci.ExtendedCommitInfo{Votes: votes[:thr-1]})
	require.NoError(t, err)
	h.collect(10, injSub)
	require.Len(t, h.cache.SharesFor(10, intentID), thr-1)
	_, err = scheme.Combine(h.cache.SharesFor(10, intentID), ct)
	require.ErrorIs(t, err, threshold.ErrInsufficientShares, "t-1 shares must not decrypt")

	// the full set is collected and DECRYPTS to the original plaintext.
	injFull, err := marshalInjected(abci.ExtendedCommitInfo{Votes: votes})
	require.NoError(t, err)
	h.collect(11, injFull)
	shares := h.cache.SharesFor(11, intentID)
	require.Len(t, shares, n)
	out, err := scheme.Combine(shares, ct)
	require.NoError(t, err)
	require.Equal(t, plaintext, out, "collected vote-extension shares must decrypt the ciphertext")

	// per-block isolation: shares are keyed by height; a stale block leaks nothing.
	require.Nil(t, h.cache.SharesFor(99, intentID))
	require.Nil(t, h.cache.SharesFor(10, intentID), "height 10 was overwritten by the height-11 collect")
}

// TestCollectIsDeterministicAndDropsGarbage proves collect() is deterministic and
// tolerant: a malformed/empty extension is skipped (it cannot poison the set —
// the BeginBlock pairing gate is the authoritative filter) and identical input
// yields identical grouping.
func TestCollectIsDeterministicAndDropsGarbage(t *testing.T) {
	good := veiltypes.VoteExtensionShares{Shares: []veiltypes.PartialRevealShare{
		{IntentId: "i1", DkgIndex: 0, Share: []byte("s0")},
		{IntentId: "i2", DkgIndex: 0, Share: []byte("s0b")},
	}}
	goodBz, err := good.Marshal()
	require.NoError(t, err)

	eci := abci.ExtendedCommitInfo{Votes: []abci.ExtendedVoteInfo{
		{BlockIdFlag: cmtproto.BlockIDFlagCommit, VoteExtension: goodBz},
		{BlockIdFlag: cmtproto.BlockIDFlagCommit, VoteExtension: []byte("not-a-proto")}, // dropped
		{BlockIdFlag: cmtproto.BlockIDFlagAbsent, VoteExtension: goodBz},                // not a commit vote → skipped
	}}
	inj, err := marshalInjected(eci)
	require.NoError(t, err)

	h := &RevealVoteExtensions{cache: NewRevealShareCache()}
	h.collect(7, inj)
	require.Len(t, h.cache.SharesFor(7, "i1"), 1)
	require.Len(t, h.cache.SharesFor(7, "i2"), 1)

	h2 := &RevealVoteExtensions{cache: NewRevealShareCache()}
	h2.collect(7, inj)
	require.Equal(t, h.cache.SharesFor(7, "i1"), h2.cache.SharesFor(7, "i1"))
}

// --- ProcessProposal unforgeability (the elevated STOP #2 artifact) ---

// testValStore is an in-test baseapp.ValidatorStore mapping consensus addresses
// to their CometBFT public keys.
type testValStore map[string]cmtprotocrypto.PublicKey

func (s testValStore) GetPubKeyByConsAddr(_ context.Context, addr sdk.ConsAddress) (cmtprotocrypto.PublicKey, error) {
	return s[addr.String()], nil
}

type veTestValidator struct {
	consAddr sdk.ConsAddress
	tmPk     cmtprotocrypto.PublicKey
	priv     cmtsecp256k1.PrivKey
}

func newVETestValidator() veTestValidator {
	priv := cmtsecp256k1.GenPrivKey()
	pub := priv.PubKey()
	return veTestValidator{
		consAddr: sdk.ConsAddress(pub.Address()),
		tmPk:     cmtprotocrypto.PublicKey{Sum: &cmtprotocrypto.PublicKey_Secp256K1{Secp256K1: pub.Bytes()}},
		priv:     priv,
	}
}

func marshalDelimited(msg proto.Message) []byte {
	var buf bytes.Buffer
	if err := protoio.NewDelimitedWriter(&buf).WriteMsg(msg); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// signedExtendedCommit builds a validly-signed ExtendedCommitInfo over the given
// per-validator vote-extension payloads at height curH (extensions are signed for
// height curH-1), and the matching last-commit CometInfo, mirroring the SDK's own
// ValidateVoteExtensions test scaffolding.
func signedExtendedCommit(vals []veTestValidator, powers []int64, veBytes [][]byte, curH int64) (abci.ExtendedCommitInfo, comet.BlockInfo) {
	votes := make([]abci.ExtendedVoteInfo, len(vals))
	for i, v := range vals {
		cve := cmtproto.CanonicalVoteExtension{
			Extension: veBytes[i],
			Height:    curH - 1,
			Round:     0,
			ChainId:   veTestChainID,
		}
		sig, err := v.priv.Sign(marshalDelimited(&cve))
		if err != nil {
			panic(err)
		}
		votes[i] = abci.ExtendedVoteInfo{
			Validator:          abci.Validator{Address: v.consAddr.Bytes(), Power: powers[i]},
			VoteExtension:      veBytes[i],
			ExtensionSignature: sig,
			BlockIdFlag:        cmtproto.BlockIDFlagCommit,
		}
	}
	ec := abci.ExtendedCommitInfo{Round: 0, Votes: votes}
	sort.Sort(veExtVoteInfos(ec.Votes))
	lc := abci.CommitInfo{Round: ec.Round, Votes: make([]abci.VoteInfo, len(ec.Votes))}
	for i, vote := range ec.Votes {
		lc.Votes[i] = abci.VoteInfo{Validator: abci.Validator{Address: vote.Validator.Address, Power: vote.Validator.Power}}
	}
	return ec, baseapp.NewBlockInfo(nil, nil, nil, lc)
}

type veExtVoteInfos []abci.ExtendedVoteInfo

func (v veExtVoteInfos) Len() int      { return len(v) }
func (v veExtVoteInfos) Swap(i, j int) { v[i], v[j] = v[j], v[i] }
func (v veExtVoteInfos) Less(i, j int) bool {
	if v[i].Validator.Power == v[j].Validator.Power {
		return bytes.Compare(v[i].Validator.Address, v[j].Validator.Address) == -1
	}
	return v[i].Validator.Power > v[j].Validator.Power
}

// TestRevealVoteExtensionUnforgeable proves the injected-Txs[0] distinction is
// unforgeable (the elevated STOP #2 artifact): a valid injected commit-info is
// ACCEPTED and STRIPPED before the lane handler runs; a normal/garbage Txs[0]
// cannot masquerade as injected-info (rejected); a missing injection is rejected;
// and a forged extension signature is rejected by ValidateVoteExtensions.
func TestRevealVoteExtensionUnforgeable(t *testing.T) {
	vals := []veTestValidator{newVETestValidator(), newVETestValidator(), newVETestValidator()}
	powers := []int64{333, 333, 334}
	store := testValStore{}
	for _, v := range vals {
		store[v.consAddr.String()] = v.tmPk
	}

	h := &RevealVoteExtensions{valStore: store, logger: log.NewNopLogger()}

	const curH = int64(3) // VE enabled at height 2 ⇒ injection active at 3
	ctx := sdk.Context{}.
		WithConsensusParams(cmtproto.ConsensusParams{Abci: &cmtproto.ABCIParams{VoteExtensionsEnableHeight: 2}}).
		WithHeaderInfo(header.Info{Height: curH, ChainID: veTestChainID}).
		WithBlockHeight(curH).
		WithLogger(log.NewNopLogger())

	// a real lane tx the proposal also carries (must survive the strip).
	laneTx := []byte("real-lane-tx")

	// the inner (Block-SDK) handler records what it received post-strip.
	var innerSawTxs [][]byte
	inner := func(_ sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		innerSawTxs = req.Txs
		return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
	}
	process := h.ProcessProposalHandler(inner)

	// each validator's VE payload (empty share set is fine for the transport gate;
	// authenticity of the COMMIT, not the share contents, is what's validated here).
	emptyPayload, err := (&veiltypes.VoteExtensionShares{}).Marshal()
	require.NoError(t, err)
	veBytes := [][]byte{emptyPayload, emptyPayload, emptyPayload}
	ec, info := signedExtendedCommit(vals, powers, veBytes, curH)
	ctxValid := ctx.WithCometInfo(info)
	injected, err := marshalInjected(ec)
	require.NoError(t, err)

	t.Run("valid injection accepted and stripped", func(t *testing.T) {
		innerSawTxs = nil
		resp, err := process(ctxValid, &abci.RequestProcessProposal{Height: curH, Txs: [][]byte{injected, laneTx}})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
		require.Equal(t, [][]byte{laneTx}, innerSawTxs, "Txs[0] injection must be stripped before the lane handler")
	})

	t.Run("normal tx at Txs[0] cannot masquerade as injection", func(t *testing.T) {
		resp, err := process(ctxValid, &abci.RequestProcessProposal{Height: curH, Txs: [][]byte{laneTx, laneTx}})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status, "a non-InjectedRevealVotes Txs[0] must be rejected")
	})

	t.Run("missing injection rejected", func(t *testing.T) {
		resp, err := process(ctxValid, &abci.RequestProcessProposal{Height: curH, Txs: [][]byte{}})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
	})

	t.Run("forged extension signature rejected", func(t *testing.T) {
		// re-sign with a NON-committee key → ValidateVoteExtensions fails.
		forger := newVETestValidator()
		forged := []veTestValidator{vals[0], vals[1], forger}
		ecF, infoF := signedExtendedCommit(forged, powers, veBytes, curH)
		injF, err := marshalInjected(ecF)
		require.NoError(t, err)
		resp, err := process(ctx.WithCometInfo(infoF), &abci.RequestProcessProposal{Height: curH, Txs: [][]byte{injF, laneTx}})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status, "an extension not signed by the committee key must be rejected")
	})
}

// TestInjectedRoundTrip pins the InjectedRevealVotes ⊃ ExtendedCommitInfo wire
// round-trip (the Txs[0] encoding).
func TestInjectedRoundTrip(t *testing.T) {
	ec := abci.ExtendedCommitInfo{Round: 2, Votes: []abci.ExtendedVoteInfo{
		{BlockIdFlag: cmtproto.BlockIDFlagCommit, VoteExtension: []byte("x"), Validator: abci.Validator{Power: 5}},
	}}
	bz, err := marshalInjected(ec)
	require.NoError(t, err)
	got, err := unmarshalInjected(bz)
	require.NoError(t, err)
	require.Equal(t, ec.Round, got.Round)
	require.Len(t, got.Votes, 1)
	require.Equal(t, int64(5), got.Votes[0].Validator.Power)

	_, err = unmarshalInjected([]byte("not-injected-votes"))
	require.Error(t, err, "a non-InjectedRevealVotes blob must fail to decode")
}
