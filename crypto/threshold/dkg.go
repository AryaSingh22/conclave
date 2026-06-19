package threshold

import (
	"errors"
	"fmt"

	"github.com/drand/kyber"
	"github.com/drand/kyber/share/dkg"
	"github.com/drand/kyber/sign/schnorr"
	"github.com/drand/kyber/util/random"

	"github.com/AryaSingh22/conclave/crypto/reveal"
)

// EpochKeyMaterial is one validator's LOCAL output of the epoch DKG. The secret
// Share never leaves the participant that produced it (D-062, FLAG II): only
// EpochPub and Commits are public. The reveal-side Scheme (NewScheme) is built
// from the public Commits; PartialReveal consumes the local Share.
type EpochKeyMaterial struct {
	Index    int
	Share    reveal.KeyShare    // node-local secret share (marshaled PriShare)
	EpochPub reveal.EpochPubKey // epoch master key = Commits[0] marshaled (G2)
	Commits  []kyber.Point      // public verification polynomial coefficients (G2)
}

// DKGParticipant is one validator's local handle on the multi-round Pedersen
// DKG (D-062 redesign of the Phase-0 EpochSetup). It wraps drand/kyber's
// share/dkg.DistKeyGenerator: the protocol and all cryptography are
// LIBRARY-provided; this type is the AUTHORED round-orchestration adapter, and
// it exists precisely so that no single node ever holds all shares — each
// participant runs its own DKGParticipant and exchanges only the public bundles.
//
// Round shape (one working session = one epoch boundary):
//
//	Round1Deal()              -> *dkg.DealBundle    (broadcast)
//	Round2Response([]Deal)    -> *dkg.ResponseBundle (broadcast)
//	Finalize([]Response)      -> *EpochKeyMaterial   (node-local share + public key)
//
// In Task 1.3 the bundles are passed in-process by RunDKGInProcess; the real
// message transport (on-chain tx vs vote-extension vs side channel) is the
// highest-risk decision deferred to the Task 1.5 design sub-review.
type DKGParticipant struct {
	index int
	group kyber.Group
	gen   *dkg.DistKeyGenerator
}

// NewDKGParticipant builds a participant for a fresh epoch DKG over `group`
// (the BLS12-381 G2 group, where epoch public keys live). `nodes` is the public
// roster (index + long-term public key) shared by all participants; `longTerm`
// is THIS node's secret; `nonce` must be identical across participants for one
// run (replay protection) and is produced once by dkg.GetNonce().
func NewDKGParticipant(group kyber.Group, index int, longTerm kyber.Scalar, nodes []dkg.Node, t int, nonce []byte) (*DKGParticipant, error) {
	suite, ok := group.(dkg.Suite)
	if !ok {
		return nil, errors.New("threshold: group does not satisfy dkg.Suite")
	}
	auth, ok := group.(schnorr.Suite)
	if !ok {
		return nil, errors.New("threshold: group does not satisfy schnorr.Suite")
	}
	gen, err := dkg.NewDistKeyHandler(&dkg.Config{
		Suite:     suite,
		Longterm:  longTerm,
		NewNodes:  nodes,
		Threshold: t,
		Auth:      schnorr.NewScheme(auth),
		Nonce:     nonce,
	})
	if err != nil {
		return nil, fmt.Errorf("threshold: dkg init: %w", err)
	}
	return &DKGParticipant{index: index, group: group, gen: gen}, nil
}

// Round1Deal produces this node's deal bundle (broadcast to all participants).
func (p *DKGParticipant) Round1Deal() (*dkg.DealBundle, error) { return p.gen.Deals() }

// Round2Response processes all deal bundles and produces this node's response
// bundle (broadcast). A nil response means nothing to complain about.
func (p *DKGParticipant) Round2Response(deals []*dkg.DealBundle) (*dkg.ResponseBundle, error) {
	return p.gen.ProcessDeals(deals)
}

// Finalize processes all response bundles and, on the all-honest path, returns
// this node's local key material. A required justification round (a faulty
// dealer) is out of scope for the offline 1.3 harness and surfaces as an error;
// churn/fault handling is exercised in Task 1.4 / 1.5.
func (p *DKGParticipant) Finalize(resps []*dkg.ResponseBundle) (*EpochKeyMaterial, error) {
	res, just, err := p.gen.ProcessResponses(resps)
	if err != nil {
		return nil, fmt.Errorf("threshold: dkg responses: %w", err)
	}
	if just != nil {
		return nil, errors.New("threshold: DKG requires a justification round (faulty dealer); not handled in the offline 1.3 harness")
	}
	if res == nil || res.Key == nil {
		return nil, errors.New("threshold: DKG did not produce a key share")
	}
	dks := res.Key
	ks, err := encodeKeyShare(dks.Share)
	if err != nil {
		return nil, err
	}
	if len(dks.Commits) == 0 {
		return nil, errors.New("threshold: DKG result has no commitments")
	}
	epk, err := dks.Commits[0].MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &EpochKeyMaterial{
		Index:    p.index,
		Share:    ks,
		EpochPub: epk,
		Commits:  dks.Commits,
	}, nil
}

// RunDKGInProcess runs the full multi-round DKG across n simulated participants
// IN-PROCESS, passing ONLY the public bundles between them — no participant ever
// observes another's secret share (D-062). It returns each participant's local
// EpochKeyMaterial (all sharing the same EpochPub / Commits). This is the
// offline test/harness driver for Tasks 1.3 and 1.4; real transport is 1.5/1.6.
func RunDKGInProcess(n, t int) ([]*EpochKeyMaterial, error) {
	if n <= 0 || t <= 0 || t > n {
		return nil, fmt.Errorf("threshold: invalid (n=%d, t=%d)", n, t)
	}
	group := newSuite().G2()

	nodes := make([]dkg.Node, n)
	longterms := make([]kyber.Scalar, n)
	for i := 0; i < n; i++ {
		sc := group.Scalar().Pick(random.New())
		longterms[i] = sc
		nodes[i] = dkg.Node{Index: dkg.Index(i), Public: group.Point().Mul(sc, nil)}
	}

	nonce := dkg.GetNonce()
	parts := make([]*DKGParticipant, n)
	for i := 0; i < n; i++ {
		p, err := NewDKGParticipant(group, i, longterms[i], nodes, t, nonce)
		if err != nil {
			return nil, err
		}
		parts[i] = p
	}

	// Round 1 — deals.
	deals := make([]*dkg.DealBundle, 0, n)
	for _, p := range parts {
		d, err := p.Round1Deal()
		if err != nil {
			return nil, err
		}
		deals = append(deals, d)
	}

	// Round 2 — responses.
	resps := make([]*dkg.ResponseBundle, 0, n)
	for _, p := range parts {
		r, err := p.Round2Response(deals)
		if err != nil {
			return nil, err
		}
		if r != nil {
			resps = append(resps, r)
		}
	}

	// Round 3 — finalize (each node derives its own local material).
	out := make([]*EpochKeyMaterial, 0, n)
	for _, p := range parts {
		m, err := p.Finalize(resps)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
