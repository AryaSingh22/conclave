package threshold

import (
	"crypto/sha256"
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
// Round shape (one working session = one epoch boundary). The honest-majority
// path is three rounds; a faulty dealer adds the justification round (Task 1.7,
// D-070 — closing the D-068/D-069 justification-grief seam so one faulty dealer
// cannot veto the epoch):
//
//	Round1Deal()                   -> *dkg.DealBundle          (broadcast, deal window)
//	Round2Response([]Deal)         -> *dkg.ResponseBundle      (broadcast iff complaints, response window)
//	ProcessResponses([]Response)   -> (material | *Justification | wait)
//	Finalize([]Justification)      -> *EpochKeyMaterial        (after the justify window)
//
// ProcessResponses returns the local material directly on the all-honest path
// (no justify window needed); when a dealer is complained about it yields that
// dealer's justification bundle (justify window), and every node then calls
// Finalize over the collected justifications. kyber excludes a dealer that
// cannot justify and computes the master secret over the QUALified set, ABORTING
// (so the epoch FAILEDs) if fewer than t dealers remain qualified — the security
// floor (build condition 3) is enforced inside kyber, not re-derived here.
//
// The on-chain transport (x/epoch, D-068) carries each bundle as opaque bytes;
// the off-chain DKG daemon (Task 1.7) drives these rounds on a live node.
type DKGParticipant struct {
	index    int
	group    kyber.Group
	gen      *dkg.DistKeyGenerator
	longTerm kyber.Scalar // retained only for the offline faulty-dealer re-sign
}

// NewDKGParticipant builds a participant for a fresh epoch DKG over `group`
// (the BLS12-381 G2 group, where epoch public keys live). `nodes` is the public
// roster (index + long-term public key) shared by all participants; `longTerm`
// is THIS node's secret; `nonce` must be identical across participants for one
// run (replay protection) — on a live network it is DeriveNonce(epoch, roster),
// a deterministic function every node computes identically.
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
	return &DKGParticipant{index: index, group: group, gen: gen, longTerm: longTerm}, nil
}

// Index reports this participant's DKG index (= its on-chain committee dkg_index).
func (p *DKGParticipant) Index() int { return p.index }

// Round1Deal produces this node's deal bundle (broadcast to all participants).
func (p *DKGParticipant) Round1Deal() (*dkg.DealBundle, error) { return p.gen.Deals() }

// Round2Response processes all deal bundles and produces this node's response
// bundle (broadcast). A nil response means nothing to complain about (the
// honest-majority path emits no response bundle at all).
func (p *DKGParticipant) Round2Response(deals []*dkg.DealBundle) (*dkg.ResponseBundle, error) {
	return p.gen.ProcessDeals(deals)
}

// ProcessResponses consumes the collected response bundles. It returns exactly
// one of three outcomes (mirroring kyber's ProcessResponses triplet):
//   - (material, nil, nil): the DKG finished on the all-honest path — no
//     complaints, no justify window needed; `material` is this node's local key.
//   - (nil, justify, nil): a dealer (possibly this node) was complained about;
//     `justify` is THIS node's justification bundle to broadcast in the justify
//     window. The caller must still call Finalize after collecting justifications.
//   - (nil, nil, nil): a justify window is needed but this node has nothing to
//     justify; it waits and calls Finalize over the collected justifications.
func (p *DKGParticipant) ProcessResponses(resps []*dkg.ResponseBundle) (*EpochKeyMaterial, *dkg.JustificationBundle, error) {
	res, jb, err := p.gen.ProcessResponses(resps)
	if err != nil {
		return nil, nil, fmt.Errorf("threshold: dkg responses: %w", err)
	}
	if res != nil {
		mat, err := resultToMaterial(p.index, res)
		return mat, nil, err
	}
	return nil, jb, nil
}

// Finalize closes the DKG after the justify window: it processes the collected
// justification bundles, excludes any dealer that could not justify, and returns
// this node's local key material over the QUALified set. kyber returns an error
// (→ the epoch FAILEDs) if fewer than t dealers remain qualified — the security
// floor (build condition 3, D-070). Call this ONLY when ProcessResponses entered
// the justify path (returned a justification or a wait); on the all-honest path
// ProcessResponses already produced the material.
func (p *DKGParticipant) Finalize(justifs []*dkg.JustificationBundle) (*EpochKeyMaterial, error) {
	res, err := p.gen.ProcessJustifications(justifs)
	if err != nil {
		return nil, fmt.Errorf("threshold: dkg justifications: %w", err)
	}
	if res == nil || res.Key == nil {
		return nil, errors.New("threshold: DKG did not produce a key share after justifications")
	}
	return resultToMaterial(p.index, res)
}

// resultToMaterial converts a kyber DKG Result into this node's EpochKeyMaterial
// (the node-local secret share + the public epoch key and commitments).
func resultToMaterial(index int, res *dkg.Result) (*EpochKeyMaterial, error) {
	dks := res.Key
	if dks == nil {
		return nil, errors.New("threshold: DKG result has no key")
	}
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
		Index:    index,
		Share:    ks,
		EpochPub: epk,
		Commits:  dks.Commits,
	}, nil
}

// --- live-network setup: long-term keys, roster, deterministic nonce ---

// nonceDomain separates the epoch-DKG nonce derivation from any other hash use.
const nonceDomain = "conclave/epoch/dkg/nonce/v1"

// DKGGroup is the group epoch-DKG public material lives in: BLS12-381 G2 (the
// same group as the epoch master key). The daemon marshals long-term keys and
// roster points over this group.
func DKGGroup() kyber.Group { return newSuite().G2() }

// GenerateLongTermKey produces a fresh long-term DKG keypair over G2 for one
// validator. The secret stays node-local (written to the node's private key
// store); the public is registered on-chain (Task 1.7 A2 registration round) so
// every committee member can build the same roster. Returns marshaled bytes.
func GenerateLongTermKey() (secret []byte, public []byte, err error) {
	g := DKGGroup()
	sc := g.Scalar().Pick(random.New())
	pub := g.Point().Mul(sc, nil)
	sb, err := sc.MarshalBinary()
	if err != nil {
		return nil, nil, err
	}
	pb, err := pub.MarshalBinary()
	if err != nil {
		return nil, nil, err
	}
	return sb, pb, nil
}

// LongTermPublicFromSecret recomputes the marshaled G2 public key for a marshaled
// long-term secret — so a node can re-derive its registration value from its
// stored secret without persisting the public separately.
func LongTermPublicFromSecret(secret []byte) ([]byte, error) {
	g := DKGGroup()
	sc := g.Scalar()
	if err := sc.UnmarshalBinary(secret); err != nil {
		return nil, fmt.Errorf("threshold: parse long-term secret: %w", err)
	}
	return g.Point().Mul(sc, nil).MarshalBinary()
}

// DeriveNonce derives the DKG run nonce deterministically from the epoch id and
// the ordered roster of long-term public keys, so every committee member uses an
// identical nonce without a separate agreement round (kyber requires Config.Nonce
// to match across participants). It is unique per epoch (the id is mixed in) and
// per roster (a different committee yields a different nonce — replay protection).
func DeriveNonce(epochID uint64, roster [][]byte) []byte {
	h := sha256.New()
	h.Write([]byte(nonceDomain))
	var idb [8]byte
	for i := 0; i < 8; i++ {
		idb[7-i] = byte(epochID >> (8 * i))
	}
	h.Write(idb[:])
	for _, pk := range roster {
		var l [4]byte
		n := uint32(len(pk))
		l[0], l[1], l[2], l[3] = byte(n>>24), byte(n>>16), byte(n>>8), byte(n)
		h.Write(l[:])
		h.Write(pk)
	}
	return h.Sum(nil)
}

// NewParticipantFromRoster builds this node's DKGParticipant from on-chain
// inputs: its committee position (myIndex == dkg_index), its node-local
// long-term secret bytes, and the ordered roster of marshaled long-term public
// keys (roster[i] is the long-term public of dkg_index i). The nonce is derived
// deterministically from (epochID, roster). This is the constructor the off-chain
// DKG daemon (Task 1.7) calls per FORMING epoch.
func NewParticipantFromRoster(epochID uint64, myIndex int, longTermSecret []byte, roster [][]byte, t int) (*DKGParticipant, error) {
	g := DKGGroup()
	if myIndex < 0 || myIndex >= len(roster) {
		return nil, fmt.Errorf("threshold: my index %d out of roster range %d", myIndex, len(roster))
	}
	sc := g.Scalar()
	if err := sc.UnmarshalBinary(longTermSecret); err != nil {
		return nil, fmt.Errorf("threshold: parse long-term secret: %w", err)
	}
	nodes := make([]dkg.Node, len(roster))
	for i, pk := range roster {
		pt := g.Point()
		if err := pt.UnmarshalBinary(pk); err != nil {
			return nil, fmt.Errorf("threshold: parse roster public %d: %w", i, err)
		}
		nodes[i] = dkg.Node{Index: dkg.Index(i), Public: pt}
	}
	nonce := DeriveNonce(epochID, roster)
	return NewDKGParticipant(g, myIndex, sc, nodes, t, nonce)
}

// --- in-process drivers (offline harness; real transport is x/epoch) ---

// NewInProcessCommittee performs the shared public SETUP for a fresh epoch DKG
// and returns the n participant handles, so a TRANSPORT layer can drive the
// rounds itself (serialize each bundle, route it through chain state,
// deserialize) rather than passing bundles directly in-process. The setup all
// participants must agree on — the long-term keypairs, the dkg.Node roster, and
// the shared nonce — is done here once; the secret shares are still produced
// node-locally inside each participant's ProcessResponses/Finalize (D-062),
// never aggregated.
//
// This is the offline validator-setup driver for the Task-1.5 on-chain DKG
// transport harness (D-068); RunDKGInProcess builds on it for the simpler
// in-process path. The DKG runs over the BLS12-381 G2 group (where epoch public
// keys live).
func NewInProcessCommittee(n, t int) ([]*DKGParticipant, error) {
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
	return parts, nil
}

// RunDKGInProcess runs the full multi-round DKG across n simulated participants
// IN-PROCESS, passing ONLY the public bundles between them — no participant ever
// observes another's secret share (D-062). It returns each participant's local
// EpochKeyMaterial (all sharing the same EpochPub / Commits). This is the
// all-honest offline driver for Tasks 1.3–1.7; the faulty-dealer path is
// RunDKGInProcessFaulty.
func RunDKGInProcess(n, t int) ([]*EpochKeyMaterial, error) {
	parts, err := NewInProcessCommittee(n, t)
	if err != nil {
		return nil, err
	}
	return driveInProcess(parts, nil)
}

// RunDKGInProcessFaulty runs the DKG with the dealers at `faulty` corrupting
// their deals (encrypted shares mangled, bundle re-signed so it stays authentic)
// and refusing to justify — the Byzantine-dealer model. It returns the local
// material of the HONEST participants (the faulty ones are excluded from QUAL and
// may error). Used by the offline justification proof (build condition 1/3): with
// few enough faults the DKG still completes excluding them; with too many it
// returns an error (kyber abort → the epoch FAILEDs). The faulty set is given as
// dkg indices.
func RunDKGInProcessFaulty(n, t int, faulty map[int]bool) ([]*EpochKeyMaterial, error) {
	parts, err := NewInProcessCommittee(n, t)
	if err != nil {
		return nil, err
	}
	return driveInProcess(parts, faulty)
}

// driveInProcess executes the deal→response→justify→finalize rounds across the
// participant handles, optionally corrupting the `faulty` dealers' deals and
// suppressing their justifications. Honest participants' material is returned;
// the faulty participants are excluded.
func driveInProcess(parts []*DKGParticipant, faulty map[int]bool) ([]*EpochKeyMaterial, error) {
	// Round 1 — deals (faulty dealers corrupt their bundle but keep it authentic).
	deals := make([]*dkg.DealBundle, 0, len(parts))
	for _, p := range parts {
		d, err := p.Round1Deal()
		if err != nil {
			return nil, err
		}
		if faulty[p.index] {
			CorruptDealForTest(p, d)
		}
		deals = append(deals, d)
	}

	// Round 2 — responses (honest holders complain about corrupted deals).
	resps := make([]*dkg.ResponseBundle, 0, len(parts))
	for _, p := range parts {
		if faulty[p.index] {
			// a faulty dealer that also withholds its response still gets its deal
			// processed by honest holders; let it process deals so its state is
			// consistent, but drop its response.
			_, _ = p.Round2Response(deals)
			continue
		}
		r, err := p.Round2Response(deals)
		if err != nil {
			return nil, err
		}
		if r != nil {
			resps = append(resps, r)
		}
	}

	// Round 3 — process responses; collect justifications from honest dealers
	// (faulty dealers refuse to justify).
	type pending struct {
		p        *DKGParticipant
		material *EpochKeyMaterial
		needFin  bool
	}
	var pend []pending
	justifs := make([]*dkg.JustificationBundle, 0, len(parts))
	for _, p := range parts {
		if faulty[p.index] {
			_, _, _ = p.ProcessResponses(resps) // advance state; ignore (won't justify)
			continue
		}
		mat, jb, err := p.ProcessResponses(resps)
		if err != nil {
			return nil, err
		}
		if jb != nil {
			justifs = append(justifs, jb)
		}
		pend = append(pend, pending{p: p, material: mat, needFin: mat == nil})
	}

	// Round 4 — finalize over the justify set for nodes that did not finish.
	out := make([]*EpochKeyMaterial, 0, len(parts))
	for _, pe := range pend {
		if !pe.needFin {
			out = append(out, pe.material)
			continue
		}
		m, err := pe.p.Finalize(justifs)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// CorruptDealForTest mangles a dealer's encrypted shares so honest holders find
// them inconsistent with the public polynomial and complain, then re-signs the
// bundle so it stays an AUTHENTIC message from that dealer (the realistic
// Byzantine-dealer fault: a validly-signed but cryptographically-bad deal, not a
// garbage blob a parser would reject). It exercises the justification round. Used
// by the offline faulty-dealer driver, the x/epoch keeper faulty test, AND the
// live localnet's --dkg-faulty-deal adversary mode (Task 1.7, D-070).
func CorruptDealForTest(p *DKGParticipant, d *dkg.DealBundle) {
	for i := range d.Deals {
		if len(d.Deals[i].EncryptedShare) > 0 {
			d.Deals[i].EncryptedShare[0] ^= 0xFF
		}
	}
	// re-sign over the mangled bundle so it stays AUTHENTIC (passes the dealer
	// signature check in ProcessDeals) and triggers complaints rather than a
	// signature rejection — exercising the justification round specifically.
	auth := schnorr.NewScheme(p.group.(schnorr.Suite))
	if sig, err := auth.Sign(p.longTerm, d.Hash()); err == nil {
		d.Signature = sig
	}
}
