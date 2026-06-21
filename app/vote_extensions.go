package app

import (
	"fmt"
	"sync"

	"cosmossdk.io/log"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/crypto/reveal"
	"github.com/AryaSingh22/conclave/crypto/threshold"
	veilkeeper "github.com/AryaSingh22/conclave/x/veil/keeper"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// vote_extensions.go wires the Task-1.6 vote-extension share transport (AUDIT
// F-4 / PHASE1_DESIGN §2). This is the consensus-critical transport, reviewed in
// isolation at STOP #2 BEFORE the StubScheme→real-scheme Reveal cutover.
//
// # The mechanism and why F-4 holds by construction
//
// A reveal share for a ciphertext is produced ONLY in ExtendVote, against
// COMMITTED state — so a valid share cannot exist until the seal that fixed the
// ciphertext's ordering is committed. ABCI++ delivers height-h vote extensions
// to exactly one place: PrepareProposal(h+1).LocalLastCommit (the proposer of
// h+1). For every node to act on those shares deterministically, the proposer
// INJECTS h's ExtendedCommitInfo into block h+1 at Txs[0], ProcessProposal(h+1)
// re-validates it with baseapp.ValidateVoteExtensions, and PreBlocker(h+1)
// collects the shares before the tx loop / BeginBlock.
//
// # Unforgeability of the injected Txs[0] (STOP #2 gating artifact)
//
// The chain distinguishes "Txs[0] is injected commit-info" from "Txs[0] is a
// normal tx" by POSITION + HEIGHT, not by content-sniffing, and the distinction
// is unforgeable:
//   - At injection-active heights ProcessProposal REQUIRES Txs[0] to decode as
//     InjectedRevealVotes and pass ValidateVoteExtensions (consensus-key
//     signatures over the canonical height-(h-1) extension + ≥2/3 power), else
//     REJECT. A normal tx placed at index 0 fails ValidateVoteExtensions → the
//     block is rejected by every honest node (can't smuggle a tx as inject-info).
//   - ValidateVoteExtensions authenticates each extension against the validator's
//     consensus key, so a proposer cannot fabricate or alter another validator's
//     shares (can't forge injected-info).
//   - Txs[0] is stripped before delegation to the Block-SDK ProcessProposal, and
//     consumed in PreBlocker BEFORE baseapp's FinalizeBlock tx loop; the loop
//     runs txDecoder per raw tx and only executes on decode success, so the
//     InjectedRevealVotes bytes (not an sdk.Tx) produce a harmless ErrTxDecode
//     result and never execute. Even in the pathological case the bytes also
//     decoded as a TxRaw, the ante's signature check rejects an unsigned tx — no
//     state mutation — and PreBlocker has already consumed Txs[0] regardless.
//   - Share AUTHENTICITY does not even rest on the VE signature: BeginBlock's
//     pairing gate (D-066) independently excludes any share failing the intrinsic
//     check, a second independent barrier.

// VoteExtensionsEnableHeight is the genesis height at which CometBFT begins
// requiring vote extensions (Task 1.6). A real chain MUST enable them for the
// reveal share transport to function; this is the value DefaultConsensusParams
// ships. It does NOT affect the determinism sim or the keeper unit tests, which
// build their own genesis with vote extensions OFF (SimulateFromSeed / explicit
// ConsensusParams) — so the determinism vector is unchanged by this constant.
const VoteExtensionsEnableHeight = 1

// DefaultConsensusParams returns CometBFT's default consensus params with vote
// extensions ENABLED at VoteExtensionsEnableHeight — the production default
// genesis value for a Conclave chain (the reveal share transport needs VEs).
// The in-process transport harness uses it in InitChain; production genesis sets
// the same `consensus.params.abci.vote_extensions_enable_height`.
func DefaultConsensusParams() *cmtproto.ConsensusParams {
	cp := cmttypes.DefaultConsensusParams().ToProto()
	cp.Abci.VoteExtensionsEnableHeight = VoteExtensionsEnableHeight
	return &cp
}

// ShareProvider supplies THIS node's local DKG secret share for an epoch — the
// node-local material ExtendVote needs to produce a partial-decryption share.
// Secret shares are NEVER in consensus state (D-062): the off-chain DKG writes
// them to a node-local store the provider reads. Task 1.6 ships the interface +
// an in-memory impl + the in-process harness that seeds it; Task 1.7's DKG
// daemon populates it on a live network. A node with no material for an epoch
// (not in committee, or DKG incomplete) returns ok=false and contributes no
// share — the threshold tolerates up to n−t missing shares.
type ShareProvider interface {
	RevealMaterial(epochID uint64) (dkgIndex uint32, keyShare reveal.KeyShare, ok bool)
}

// MapShareProvider is a simple in-memory ShareProvider for tests and the
// in-process harness. A real node backs this with a file/local-DB store written
// by the DKG daemon (Task 1.7).
type MapShareProvider struct {
	mu  sync.RWMutex
	mat map[uint64]struct {
		idx uint32
		ks  reveal.KeyShare
	}
}

// NewMapShareProvider builds an empty in-memory share provider.
func NewMapShareProvider() *MapShareProvider {
	return &MapShareProvider{mat: map[uint64]struct {
		idx uint32
		ks  reveal.KeyShare
	}{}}
}

// Set records this node's (dkg_index, secret share) for an epoch.
func (m *MapShareProvider) Set(epochID uint64, idx uint32, ks reveal.KeyShare) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mat[epochID] = struct {
		idx uint32
		ks  reveal.KeyShare
	}{idx, ks}
}

// RevealMaterial implements ShareProvider.
func (m *MapShareProvider) RevealMaterial(epochID uint64) (uint32, reveal.KeyShare, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.mat[epochID]
	return v.idx, v.ks, ok
}

// RevealShareCache holds the reveal shares collected from one block's injected
// vote extensions — the per-block supply the Reveal consumes (Task 1.6). It
// implements veilkeeper.RevealShareSource. PreBlocker(h) populates it for height
// h from the proposer-injected, ValidateVoteExtensions-verified ExtendedCommitInfo
// of block h-1; RevealSealed (Phase B) reads it at the matching height. Keyed by
// height so a stale block's shares can never leak into another's Reveal.
type RevealShareCache struct {
	mu       sync.Mutex
	height   int64
	byIntent map[string][]reveal.RevealShare
}

// NewRevealShareCache builds an empty cache.
func NewRevealShareCache() *RevealShareCache {
	return &RevealShareCache{byIntent: map[string][]reveal.RevealShare{}}
}

// reset replaces the cache contents for a new block height.
func (c *RevealShareCache) reset(height int64, byIntent map[string][]reveal.RevealShare) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.height = height
	c.byIntent = byIntent
}

// SharesFor implements veilkeeper.RevealShareSource: the shares collected for an
// intent at a height (nil if the height does not match the cached block or the
// intent has none).
func (c *RevealShareCache) SharesFor(height int64, intentID string) []reveal.RevealShare {
	c.mu.Lock()
	defer c.mu.Unlock()
	if height != c.height {
		return nil
	}
	return c.byIntent[intentID]
}

// RevealVoteExtensions is the Task-1.6 share-transport handler set: ExtendVote /
// VerifyVoteExtension, the PrepareProposal/ProcessProposal injection wrappers,
// and the PreBlocker collector. It holds only the reads it needs and the
// node-local ShareProvider; it writes no consensus state itself (the Reveal,
// driven by veil's BeginBlocker, does).
type RevealVoteExtensions struct {
	logger   log.Logger
	veil     veilkeeper.Keeper
	epoch    EpochVerificationKeeper
	valStore baseapp.ValidatorStore
	shares   ShareProvider
	scheme   *threshold.Scheme // client scheme: only PartialReveal (no epoch material)
	cache    *RevealShareCache
}

// EpochVerificationKeeper is the x/epoch surface the transport reads: committee
// size (range-check a share's dkg_index in VerifyVoteExtension).
type EpochVerificationKeeper interface {
	CommitteeSize(ctx sdk.Context, epochID uint64) (int, bool)
}

// NewRevealVoteExtensions builds the transport handler set.
func NewRevealVoteExtensions(
	logger log.Logger,
	veil veilkeeper.Keeper,
	epoch EpochVerificationKeeper,
	valStore baseapp.ValidatorStore,
	shares ShareProvider,
	cache *RevealShareCache,
) *RevealVoteExtensions {
	return &RevealVoteExtensions{
		logger:   logger.With("module", "x/veil", "component", "reveal-vote-extensions"),
		veil:     veil,
		epoch:    epoch,
		valStore: valStore,
		shares:   shares,
		scheme:   threshold.NewClientScheme(),
		cache:    cache,
	}
}

// injectionActive reports whether height h carries injected vote-extension data
// at Txs[0]. VEs are produced from VoteExtensionsEnableHeight onward, so the
// FIRST block whose LocalLastCommit carries them is enableHeight+1: injection is
// active for h > enableHeight (matching baseapp.ValidateVoteExtensions' own
// extsEnabled gate). enableHeight==0 means VEs disabled.
func injectionActive(ctx sdk.Context, height int64) bool {
	cp := ctx.ConsensusParams()
	return cp.Abci != nil && cp.Abci.VoteExtensionsEnableHeight != 0 && height > cp.Abci.VoteExtensionsEnableHeight
}

// --- injected-info wire helpers (InjectedRevealVotes ⊃ ExtendedCommitInfo) ---

// marshalInjected wraps an ExtendedCommitInfo into the InjectedRevealVotes
// block-data element placed at Txs[0].
func marshalInjected(eci abci.ExtendedCommitInfo) ([]byte, error) {
	eciBz, err := eci.Marshal()
	if err != nil {
		return nil, err
	}
	wrapper := veiltypes.InjectedRevealVotes{ExtendedCommitInfo: eciBz}
	return wrapper.Marshal()
}

// unmarshalInjected decodes the Txs[0] element back into an ExtendedCommitInfo.
func unmarshalInjected(bz []byte) (abci.ExtendedCommitInfo, error) {
	var wrapper veiltypes.InjectedRevealVotes
	if err := wrapper.Unmarshal(bz); err != nil {
		return abci.ExtendedCommitInfo{}, err
	}
	var eci abci.ExtendedCommitInfo
	if err := eci.Unmarshal(wrapper.ExtendedCommitInfo); err != nil {
		return abci.ExtendedCommitInfo{}, err
	}
	return eci, nil
}

// ExtendVoteHandler produces THIS validator's reveal shares for every in-flight
// sealed ciphertext it can serve (PHASE1_DESIGN §2, the F-4 existence proof).
// It enumerates the committed SEALED_IN_SESSION set (the deterministic 0x02
// index — identical across nodes), PartialReveals each with the node-local
// secret share, and packs the shares into the vote extension. PartialReveal is
// randomness-free (D-066); a node missing an epoch's share simply omits it. It
// NEVER errors out the vote (an empty extension is valid): a failed ExtendVote
// must not stall the validator's precommit.
func (h *RevealVoteExtensions) ExtendVoteHandler() sdk.ExtendVoteHandler {
	return func(ctx sdk.Context, req *abci.RequestExtendVote) (*abci.ResponseExtendVote, error) {
		empty := &abci.ResponseExtendVote{VoteExtension: []byte{}}
		targets, err := h.veil.EnumerateInFlightForReveal(ctx)
		if err != nil {
			h.logger.Error("ExtendVote enumerate failed", "height", req.Height, "err", err)
			return empty, nil
		}
		var payload veiltypes.VoteExtensionShares
		for _, t := range targets {
			idx, ks, ok := h.shares.RevealMaterial(t.EpochID)
			if !ok {
				continue // this node holds no share for that epoch
			}
			share, err := h.scheme.PartialReveal(ks, t.Ciphertext)
			if err != nil {
				h.logger.Error("PartialReveal failed", "intent", t.IntentID, "epoch", t.EpochID, "err", err)
				continue
			}
			payload.Shares = append(payload.Shares, veiltypes.PartialRevealShare{
				IntentId: t.IntentID,
				DkgIndex: idx,
				Share:    share,
			})
		}
		bz, err := payload.Marshal()
		if err != nil {
			return empty, nil
		}
		return &abci.ResponseExtendVote{VoteExtension: bz}, nil
	}
}

// VerifyVoteExtensionHandler does STRUCTURAL validation only (decision 5): the
// cryptographic pairing gate is the deterministic security barrier and runs once
// in BeginBlock (D-066), not redundantly per vote here. It accepts an empty
// extension (a node with no shares), and rejects an extension that does not
// decode, carries more shares than there are in-flight ciphertexts, duplicates
// an (intent, index) pair, or references an intent/dkg_index that is not a real
// in-flight target. Deterministic (reads committed state only).
func (h *RevealVoteExtensions) VerifyVoteExtensionHandler() sdk.VerifyVoteExtensionHandler {
	accept := &abci.ResponseVerifyVoteExtension{Status: abci.ResponseVerifyVoteExtension_ACCEPT}
	reject := &abci.ResponseVerifyVoteExtension{Status: abci.ResponseVerifyVoteExtension_REJECT}
	return func(ctx sdk.Context, req *abci.RequestVerifyVoteExtension) (*abci.ResponseVerifyVoteExtension, error) {
		if len(req.VoteExtension) == 0 {
			return accept, nil
		}
		var payload veiltypes.VoteExtensionShares
		if err := payload.Unmarshal(req.VoteExtension); err != nil {
			return reject, nil
		}
		targets, err := h.veil.EnumerateInFlightForReveal(ctx)
		if err != nil {
			return reject, nil
		}
		// a node cannot serve more shares than there are in-flight ciphertexts —
		// the seal-time max_intents_per_session cap therefore bounds the VE size.
		if len(payload.Shares) > len(targets) {
			return reject, nil
		}
		epochOf := make(map[string]uint64, len(targets))
		for _, t := range targets {
			epochOf[t.IntentID] = t.EpochID
		}
		seen := make(map[string]struct{}, len(payload.Shares))
		for i := range payload.Shares {
			s := &payload.Shares[i]
			epochID, ok := epochOf[s.IntentId]
			if !ok {
				return reject, nil // not an in-flight target
			}
			n, ok := h.epoch.CommitteeSize(ctx, epochID)
			if !ok || int(s.DkgIndex) >= n {
				return reject, nil // dkg_index out of committee range
			}
			key := fmt.Sprintf("%s/%d", s.IntentId, s.DkgIndex)
			if _, dup := seen[key]; dup {
				return reject, nil
			}
			seen[key] = struct{}{}
		}
		return accept, nil
	}
}

// PrepareProposalHandler wraps the Block-SDK handler: it builds the lane block
// with a reduced byte budget (reserving room for the injection), then PREPENDS
// the prior block's ExtendedCommitInfo at Txs[0] (PHASE1_DESIGN §2). Below the
// injection-active height it is a pass-through.
func (h *RevealVoteExtensions) PrepareProposalHandler(inner sdk.PrepareProposalHandler) sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
		if !injectionActive(ctx, req.Height) {
			return inner(ctx, req)
		}
		injected, err := marshalInjected(req.LocalLastCommit)
		if err != nil {
			return nil, fmt.Errorf("reveal-ve: marshal injected commit info: %w", err)
		}
		// reserve room so prepending never overflows MaxTxBytes (the injected
		// element counts toward the block byte budget). +8 covers the slice
		// length-delimiter overhead CometBFT adds per tx.
		reserved := int64(len(injected)) + 8
		if req.MaxTxBytes > reserved {
			req.MaxTxBytes -= reserved
		} else {
			req.MaxTxBytes = 0
		}
		resp, err := inner(ctx, req)
		if err != nil {
			return nil, err
		}
		resp.Txs = append([][]byte{injected}, resp.Txs...)
		return resp, nil
	}
}

// ProcessProposalHandler wraps the Block-SDK handler: at injection-active heights
// it REQUIRES Txs[0] to be a valid, ValidateVoteExtensions-verified
// InjectedRevealVotes, strips it, and delegates the remaining txs to the inner
// (lane-structure) handler. A proposal missing/forging the injection is REJECTED
// — this is the unforgeability gate (see file header). Below the injection-active
// height it is a pass-through.
func (h *RevealVoteExtensions) ProcessProposalHandler(inner sdk.ProcessProposalHandler) sdk.ProcessProposalHandler {
	reject := &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}
	return func(ctx sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		if !injectionActive(ctx, req.Height) {
			return inner(ctx, req)
		}
		if len(req.Txs) == 0 {
			h.logger.Error("ProcessProposal: missing injected commit info at Txs[0]", "height", req.Height)
			return reject, nil
		}
		eci, err := unmarshalInjected(req.Txs[0])
		if err != nil {
			h.logger.Error("ProcessProposal: Txs[0] is not InjectedRevealVotes", "height", req.Height, "err", err)
			return reject, nil
		}
		// the unforgeability gate: authenticate every extension (consensus-key
		// signatures + ≥2/3 power). currentHeight/chainID args are ignored since
		// v0.50.5 (read from ctx).
		if err := baseapp.ValidateVoteExtensions(ctx, h.valStore, req.Height, "", eci); err != nil {
			h.logger.Error("ProcessProposal: vote-extension validation failed", "height", req.Height, "err", err)
			return reject, nil
		}
		req.Txs = req.Txs[1:]
		return inner(ctx, req)
	}
}

// PreBlocker collects the reveal shares from the injected ExtendedCommitInfo into
// the per-block cache BEFORE the tx loop / BeginBlock (PHASE1_DESIGN §2), then
// delegates to the wrapped (module-manager) PreBlocker. Collection is structural
// only — the cryptographic gate runs in BeginBlock. Deterministic: it reads the
// committed Txs[0] every node agreed on in ProcessProposal.
func (h *RevealVoteExtensions) PreBlocker(inner sdk.PreBlocker) sdk.PreBlocker {
	return func(ctx sdk.Context, req *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
		if injectionActive(ctx, req.Height) && len(req.Txs) > 0 {
			h.collect(req.Height, req.Txs[0])
		} else {
			h.cache.reset(req.Height, map[string][]reveal.RevealShare{})
		}
		return inner(ctx, req)
	}
}

// collect decodes the injected commit info and groups every validator's reveal
// shares by intent id into the per-block cache. Bad/undecodable extensions are
// skipped (a malformed extension cannot poison the set; the BeginBlock pairing
// gate is the authoritative filter).
func (h *RevealVoteExtensions) collect(height int64, injectedTx []byte) {
	byIntent := map[string][]reveal.RevealShare{}
	eci, err := unmarshalInjected(injectedTx)
	if err != nil {
		h.cache.reset(height, byIntent)
		return
	}
	for _, vote := range eci.Votes {
		if vote.BlockIdFlag != cmtproto.BlockIDFlagCommit || len(vote.VoteExtension) == 0 {
			continue
		}
		var payload veiltypes.VoteExtensionShares
		if err := payload.Unmarshal(vote.VoteExtension); err != nil {
			continue
		}
		for i := range payload.Shares {
			s := &payload.Shares[i]
			byIntent[s.IntentId] = append(byIntent[s.IntentId], reveal.RevealShare(s.Share))
		}
	}
	h.cache.reset(height, byIntent)
}
