// Package threshold implements the Phase-1 BLS12-381 threshold-encryption
// ThresholdScheme (SPEC §8) behind crypto/reveal.ThresholdScheme, plus the
// epoch DKG sub-protocol (D-062). It is OFFLINE in Task 1.3: no consensus
// wiring, and the live chain keeps using reveal.StubScheme until Task 1.6.
//
// # Construction (D-066): tlock-style timelock encryption over threshold BLS
//
// This is the published drand `tlock` construction (Gailly–Melissaris et al.):
// hybrid KEM/DEM where a per-ciphertext symmetric key is wrapped by Boneh–
// Franklin IBE to an identity, and the IBE decryption key for that identity is
// a threshold BLS signature on the identity. We adopt it rather than authoring
// a primitive.
//
//   - Orientation: signatures / IBE-decryption-keys live on G1, public keys /
//     the epoch master key live on G2 (the "sigs-on-G1" drand scheme). This is
//     the matched pair: ibe.EncryptCCAonG2(master∈G2, id, dek) is decrypted by
//     ibe.DecryptCCAonG2(σ∈G1, ·), and σ = s·H_G1(id) is exactly a BLS
//     signature on `id` under master = s·G2. The recovered threshold signature
//     on `id` IS the IBE decryption key.
//
// # Authored vs library (the audit boundary, held literally per D-066)
//
//   - LIBRARY (drand/kyber + kyber-bls12381): pairing groups + hash-to-curve,
//     secret sharing & Lagrange (share), threshold BLS sign/verify (sign/tbls),
//     the Pedersen DKG (share/dkg), and the BF-IBE primitive (encrypt/ibe).
//   - AUTHORED (this package, the §8(d)/Task-1.8 audit surface): (1) the
//     KEM/DEM hybrid wrapper (stdlib AES-256-GCM DEM around the IBE-wrapped
//     key); (2) the identity-derivation rule (deriveIdentity); (3) this
//     reveal.ThresholdScheme adapter; (4) the share-verification GATING before
//     recovery (gate / Combine). No novel cryptographic primitive is authored.
//
// # Determinism (SPEC §11.6)
//
// Encrypt is client-side/off-chain (crypto/rand for the DEK, IBE σ, AEAD
// nonce). The on-chain paths — PartialReveal (ExtendVote, Task 1.6) and Combine
// + verification (BeginBlock, Task 1.6) — take NO fresh randomness: a BLS
// signature share is deterministic (σ_i = sk_i·H(id)), the pairing-check gate
// is deterministic, and Lagrange recovery + IBE/AEAD decryption are
// deterministic given (shares, ct).
package threshold

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/drand/kyber"
	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/encrypt/ibe"
	"github.com/drand/kyber/pairing"
	"github.com/drand/kyber/share"
	"github.com/drand/kyber/sign"
	"github.com/drand/kyber/sign/tbls"

	"github.com/AryaSingh22/conclave/crypto/reveal"
)

// ibeIdentityDST domain-separates the per-ciphertext IBE identity preimage.
//
// AUTHORED (D-066 piece #2). drand tlock derives the IBE identity as a digest
// of the canonical identity-defining bytes (there: the beacon round, via
// scheme.DigestBeacon — a SHA-256 over the round encoding). Conclave clients
// cannot predict their future ordering position at encryption time, so the
// identity instead binds to the DEM body (the part of the ciphertext fixed
// BEFORE the KEM encapsulation — which breaks the circularity, since the KEM
// output depends on the identity). This is a deliberate, documented divergence
// from tlock's external-label model, forced by Conclave's architecture, not an
// improvisation of tlock's hashing — the hash-to-curve of the identity is still
// performed inside the library IBE with its own domain separation. Reviewed
// before commit.
var ibeIdentityDST = []byte("conclave/veil/ibe-identity/v1")

// dekLen is the symmetric key length (AES-256). It is <= sha256.Size, the IBE
// plaintext bound, so the DEK fits in a single IBE encapsulation.
const dekLen = 32

// gcmNonceLen is the standard AES-GCM nonce length.
const gcmNonceLen = 12

var (
	// ErrNoEpochMaterial is returned by reveal-side methods (Combine /
	// VerifyShare) on a client-only Scheme that has no DKG public material.
	ErrNoEpochMaterial = errors.New("threshold: scheme has no epoch verification material (client-only)")
	// ErrInsufficientShares means fewer than t shares survived verification.
	ErrInsufficientShares = errors.New("threshold: insufficient valid reveal shares to reach the threshold")
)

// Scheme is the BLS12-381 threshold-encryption ThresholdScheme. A client-only
// instance (NewClientScheme) can Encrypt; a reveal-capable instance (NewScheme,
// built from DKG public output) can additionally PartialReveal / Combine /
// VerifyShare. The epoch public verification material (the polynomial of
// per-validator public key shares) lives on the instance because the consuming
// Combine signature is fixed (D-062) and cannot take it as a parameter.
type Scheme struct {
	suite pairing.Suite
	tbls  sign.ThresholdScheme // signatures on G1, public keys on G2

	pubPoly *share.PubPoly // epoch verification material; nil ⇒ client-only
	n, t    int
}

var _ reveal.ThresholdScheme = (*Scheme)(nil)

// suite builds the BLS12-381 pairing suite used for IBE and tBLS. The SAME
// suite is used for both so the G1 hash-to-curve (and its DST) matches: the
// `id` bytes that ibe.EncryptCCAonG2 hashes to G1 are the exact bytes that the
// tBLS-on-G1 scheme signs, so the recovered signature is the IBE private key.
func newSuite() pairing.Suite { return bls12381.NewBLS12381Suite() }

// NewClientScheme returns an encrypt-only Scheme (no epoch material).
func NewClientScheme() *Scheme {
	s := newSuite()
	return &Scheme{suite: s, tbls: tbls.NewThresholdSchemeOnG1(s)}
}

// NewScheme builds a reveal-capable Scheme from the DKG public output: the
// epoch commitments (public polynomial coefficients in G2; commits[0] is the
// epoch master key) and the validator count n. The reconstruction threshold is
// t = len(commits) (the DKG polynomial has degree t-1).
func NewScheme(commits []kyber.Point, n int) (*Scheme, error) {
	if len(commits) == 0 {
		return nil, errors.New("threshold: empty commitments")
	}
	s := newSuite()
	return &Scheme{
		suite:   s,
		tbls:    tbls.NewThresholdSchemeOnG1(s),
		pubPoly: share.NewPubPoly(s.G2(), s.G2().Point().Base(), commits),
		n:       n,
		t:       len(commits),
	}, nil
}

// Threshold implements the SPEC §6.3 t-of-n rule: t = ⌊2n/3⌋ + 1.
func (s *Scheme) Threshold(n int) int { return 2*n/3 + 1 }

// Encrypt encrypts an intent payload to the epoch key (CLIENT-SIDE, off-chain;
// uses crypto/rand). KEM/DEM: a random DEK encrypts the payload with AES-256-GCM
// (the DEM); the DEK is IBE-wrapped to id = H(DST‖C_dem) under the epoch master
// key (the KEM). AUTHORED glue (D-066 piece #1) over the library IBE primitive.
func (s *Scheme) Encrypt(epk reveal.EpochPubKey, payload []byte) ([]byte, error) {
	master := s.suite.G2().Point()
	if err := master.UnmarshalBinary(epk); err != nil {
		return nil, fmt.Errorf("threshold: bad epoch public key: %w", err)
	}
	dek := make([]byte, dekLen)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, err
	}
	cdem, err := aeadSeal(dek, payload)
	if err != nil {
		return nil, err
	}
	id := deriveIdentity(cdem)
	ckem, err := ibe.EncryptCCAonG2(s.suite, master, id, dek)
	if err != nil {
		return nil, err
	}
	return encodeCiphertext(ckem, cdem)
}

// PartialReveal produces one validator's reveal share for a ciphertext
// (ON-CHAIN, ExtendVote in Task 1.6 — DETERMINISTIC, no fresh randomness). The
// share is a tBLS signature share on the ciphertext identity; the bytes are
// index-prefixed by tBLS and ARE the validity proof (verified by the intrinsic
// pairing check in VerifyShare/gate). AUTHORED adapter (D-066 piece #3).
func (s *Scheme) PartialReveal(ks reveal.KeyShare, ct []byte) (reveal.RevealShare, error) {
	priShare, err := decodeKeyShare(s.suite, ks)
	if err != nil {
		return nil, err
	}
	_, cdem, err := decodeCiphertext(ct)
	if err != nil {
		return nil, err
	}
	sig, err := s.tbls.Sign(priShare, deriveIdentity(cdem))
	if err != nil {
		return nil, err
	}
	return reveal.RevealShare(sig), nil
}

// VerifyShare reports whether a reveal share is a correct partial decryption
// under that validator's published key share — the INTRINSIC pairing check
// e(σ_i, g2) == e(H(id), PK_i) (tbls.VerifyPartial). This is the per-share
// validity proof (FLAG III); it carries no separate NIZK because the share
// itself is the proof. Exposed for the Task-1.4 adversarial harness and the
// Task-1.6 ExtendVote/BeginBlock gating.
func (s *Scheme) VerifyShare(rs reveal.RevealShare, ct []byte) error {
	if s.pubPoly == nil {
		return ErrNoEpochMaterial
	}
	_, cdem, err := decodeCiphertext(ct)
	if err != nil {
		return err
	}
	return s.tbls.VerifyPartial(s.pubPoly, deriveIdentity(cdem), []byte(rs))
}

// Combine recovers the plaintext from t-of-n reveal shares (ON-CHAIN,
// BeginBlock in Task 1.6 — DETERMINISTIC). It GATES every share through the
// pairing check (gate) BEFORE recovery, deterministically excluding any
// syntactically-valid-but-wrong share, then Lagrange-recovers the threshold
// signature, IBE-decrypts the DEK, and AES-GCM-opens the payload. AUTHORED
// gating (D-066 piece #4).
//
// Two INDEPENDENT barriers, two properties (see recoverAndDecrypt):
//   - LIVENESS / ROBUSTNESS — the gate: it excludes adversarial shares in O(n)
//     so recovery succeeds from ANY t honest shares. Without it, one valid-
//     looking-but-wrong share poisons the first-t Lagrange set and forces a
//     retry → eventual CANCEL of an otherwise-decryptable intent (a single-bad-
//     validator liveness DoS). This robustness is what FLAG III buys.
//   - INTEGRITY — the library IBE FO/CCA rP check (in DecryptCCAonG2): a wrong
//     recovered signature deterministically ERRORS, never silently corrupts
//     into a plausible wrong plaintext. This barrier is independent of the gate
//     and rests on the audited library, not authored code.
func (s *Scheme) Combine(shares []reveal.RevealShare, ct []byte) ([]byte, error) {
	if s.pubPoly == nil {
		return nil, ErrNoEpochMaterial
	}
	ckem, cdem, err := decodeCiphertext(ct)
	if err != nil {
		return nil, err
	}
	id := deriveIdentity(cdem)
	valid := s.gate(id, shares)
	if len(valid) < s.t {
		return nil, ErrInsufficientShares
	}
	return s.recoverAndDecrypt(id, valid, ckem, cdem)
}

// gate verifies each share with the intrinsic pairing check, drops invalid and
// duplicate-index shares, and returns the survivors sorted by index. The map is
// used only for dedup membership, never iterated into the output (the output is
// the sorted slice), so the result is deterministic (SPEC §11.6).
func (s *Scheme) gate(id []byte, shares []reveal.RevealShare) [][]byte {
	type iShare struct {
		i int
		b []byte
	}
	seen := make(map[int]struct{})
	out := make([]iShare, 0, len(shares))
	for _, rs := range shares {
		b := []byte(rs)
		idx, err := tbls.SigShare(b).Index()
		if err != nil {
			continue
		}
		if _, dup := seen[idx]; dup {
			continue
		}
		if err := s.tbls.VerifyPartial(s.pubPoly, id, b); err != nil {
			continue
		}
		seen[idx] = struct{}{}
		out = append(out, iShare{idx, b})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].i < out[b].i })
	res := make([][]byte, len(out))
	for k := range out {
		res[k] = out[k].b
	}
	return res
}

// recoverAndDecrypt does NO verification — it TRUSTS its input shares, so the
// gate (Combine) is what keeps adversarial shares out of it. A wrong share
// reaching here poisons the Lagrange interpolation (share.RecoverCommit, the raw
// non-verifying primitive) and yields a wrong threshold signature; the library
// IBE FO/CCA rP check (DecryptCCAonG2) then deterministically ERRORS on that
// wrong signature — it never silently corrupts into a wrong plaintext. So
// removing the gate does NOT cause silent corruption (integrity rests on the FO
// check); it costs LIVENESS — recovery fails on a set that contained ≥t honest
// shares (tested RED-without-gate). (Note: tbls.Recover would self-filter bad
// shares and mask this; the raw primitive keeps the gate's robustness role
// visible and testable.)
func (s *Scheme) recoverAndDecrypt(id []byte, validShares [][]byte, ckem *ibe.Ciphertext, cdem []byte) ([]byte, error) {
	pubShares := make([]*share.PubShare, 0, len(validShares))
	for _, b := range validShares {
		ss := tbls.SigShare(b)
		idx, err := ss.Index()
		if err != nil {
			return nil, err
		}
		pt := s.suite.G1().Point()
		if err := pt.UnmarshalBinary(ss.Value()); err != nil {
			return nil, err
		}
		pubShares = append(pubShares, &share.PubShare{I: idx, V: pt})
	}
	sigPoint, err := share.RecoverCommit(s.suite.G1(), pubShares, s.t, s.n)
	if err != nil {
		return nil, err
	}
	dek, err := ibe.DecryptCCAonG2(s.suite, sigPoint, ckem)
	if err != nil {
		return nil, err
	}
	return aeadOpen(dek, cdem)
}

// deriveIdentity computes the per-ciphertext IBE identity id = H(DST ‖ C_dem).
// AUTHORED (D-066 piece #2). See ibeIdentityDST.
func deriveIdentity(cdem []byte) []byte {
	h := sha256.New()
	h.Write(ibeIdentityDST)
	h.Write(cdem)
	return h.Sum(nil)
}

// --- AES-256-GCM DEM (stdlib, no new dependency) ---

func aeadSeal(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcmNonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func aeadOpen(key, blob []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcmNonceLen {
		return nil, errors.New("threshold: DEM ciphertext too short")
	}
	nonce, ct := blob[:gcmNonceLen], blob[gcmNonceLen:]
	return gcm.Open(nil, nonce, ct, nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// --- wire encoding (length-prefixed; deterministic) ---

func encodeCiphertext(ckem *ibe.Ciphertext, cdem []byte) ([]byte, error) {
	u, err := ckem.U.MarshalBinary()
	if err != nil {
		return nil, err
	}
	var buf []byte
	buf = appendChunk(buf, u)
	buf = appendChunk(buf, ckem.V)
	buf = appendChunk(buf, ckem.W)
	buf = appendChunk(buf, cdem)
	return buf, nil
}

func decodeCiphertext(b []byte) (*ibe.Ciphertext, []byte, error) {
	u, rest, err := readChunk(b)
	if err != nil {
		return nil, nil, err
	}
	v, rest, err := readChunk(rest)
	if err != nil {
		return nil, nil, err
	}
	w, rest, err := readChunk(rest)
	if err != nil {
		return nil, nil, err
	}
	cdem, rest, err := readChunk(rest)
	if err != nil {
		return nil, nil, err
	}
	if len(rest) != 0 {
		return nil, nil, errors.New("threshold: trailing bytes in ciphertext")
	}
	uPoint := newSuite().G2().Point()
	if err := uPoint.UnmarshalBinary(u); err != nil {
		return nil, nil, err
	}
	return &ibe.Ciphertext{U: uPoint, V: v, W: w}, cdem, nil
}

func appendChunk(buf, chunk []byte) []byte {
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(chunk)))
	buf = append(buf, l[:]...)
	return append(buf, chunk...)
}

func readChunk(b []byte) (chunk, rest []byte, err error) {
	if len(b) < 4 {
		return nil, nil, errors.New("threshold: truncated length prefix")
	}
	n := binary.BigEndian.Uint32(b[:4])
	if uint64(len(b)-4) < uint64(n) {
		return nil, nil, errors.New("threshold: truncated chunk")
	}
	return b[4 : 4+n], b[4+n:], nil
}

// --- KeyShare (PriShare) marshaling ---

func encodeKeyShare(ps *share.PriShare) (reveal.KeyShare, error) {
	v, err := ps.V.MarshalBinary()
	if err != nil {
		return nil, err
	}
	out := make([]byte, 4+len(v))
	binary.BigEndian.PutUint32(out[:4], uint32(ps.I))
	copy(out[4:], v)
	return out, nil
}

func decodeKeyShare(s pairing.Suite, ks reveal.KeyShare) (*share.PriShare, error) {
	if len(ks) < 4 {
		return nil, errors.New("threshold: truncated key share")
	}
	i := int(binary.BigEndian.Uint32(ks[:4]))
	sc := s.G2().Scalar()
	if err := sc.UnmarshalBinary(ks[4:]); err != nil {
		return nil, err
	}
	return &share.PriShare{I: i, V: sc}, nil
}
