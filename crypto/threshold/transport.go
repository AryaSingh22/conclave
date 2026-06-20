package threshold

// DKG bundle transport serialization (AUTHORED, OFF-CHAIN validator transport —
// D-068, Task 1.5). The Task-1.5 on-chain DKG transport (PHASE1_DESIGN §2(b),
// "Candidate A") carries kyber DealBundle / ResponseBundle bundles as OPAQUE
// bytes inside x/epoch MsgSubmitDeal / MsgSubmitResponse: the chain is a
// DETERMINISTIC RELAY keyed by (epoch id, participant index) and NEVER
// interprets a bundle — only validators (off-chain, a protocol, D-061)
// serialize/deserialize and run the kyber DKG. So this serde lives here in
// crypto/threshold (the validator-side surface), NOT in consensus code, and
// nothing here ever runs in BeginBlock/EndBlock.
//
// kyber ships no bundle (de)serializer (drand serializes these in its own repo,
// not in the library), so the wire form is authored. It is pure transport glue
// — length-prefixed fields, no cryptography — reusing the threshold.go
// appendChunk/readChunk helpers. The DKG runs over the BLS12-381 G2 group (where
// epoch public keys live), so points unmarshal there.

import (
	"encoding/binary"
	"errors"

	"github.com/drand/kyber"
	"github.com/drand/kyber/share/dkg"
)

// dkgGroup is the group DKG public points (deal public coefficients) live in:
// BLS12-381 G2, the same group the epoch master key lives in (threshold.go).
func dkgGroup() kyber.Group { return newSuite().G2() }

// MarshalDealBundle serializes a Round-1 deal bundle for on-chain transport.
func MarshalDealBundle(d *dkg.DealBundle) ([]byte, error) {
	if d == nil {
		return nil, errors.New("threshold: nil deal bundle")
	}
	var buf []byte
	buf = appendUint32(buf, d.DealerIndex)
	buf = appendUint32(buf, uint32(len(d.Public)))
	for _, p := range d.Public {
		pb, err := p.MarshalBinary()
		if err != nil {
			return nil, err
		}
		buf = appendChunk(buf, pb)
	}
	buf = appendUint32(buf, uint32(len(d.Deals)))
	for i := range d.Deals {
		buf = appendUint32(buf, d.Deals[i].ShareIndex)
		buf = appendChunk(buf, d.Deals[i].EncryptedShare)
	}
	buf = appendChunk(buf, d.SessionID)
	buf = appendChunk(buf, d.Signature)
	return buf, nil
}

// UnmarshalDealBundle reverses MarshalDealBundle. Public coefficients are
// reconstructed on the G2 group so the bundle Hash() (and thus the dealer
// signature ProcessDeals verifies) recomputes identically across the round trip.
func UnmarshalDealBundle(b []byte) (*dkg.DealBundle, error) {
	group := dkgGroup()
	di, b, err := readUint32(b)
	if err != nil {
		return nil, err
	}
	nPub, b, err := readUint32(b)
	if err != nil {
		return nil, err
	}
	pubs := make([]kyber.Point, nPub)
	for i := uint32(0); i < nPub; i++ {
		var pb []byte
		pb, b, err = readChunk(b)
		if err != nil {
			return nil, err
		}
		pt := group.Point()
		if err = pt.UnmarshalBinary(pb); err != nil {
			return nil, err
		}
		pubs[i] = pt
	}
	nDeals, b, err := readUint32(b)
	if err != nil {
		return nil, err
	}
	deals := make([]dkg.Deal, nDeals)
	for i := uint32(0); i < nDeals; i++ {
		var si uint32
		si, b, err = readUint32(b)
		if err != nil {
			return nil, err
		}
		var es []byte
		es, b, err = readChunk(b)
		if err != nil {
			return nil, err
		}
		deals[i] = dkg.Deal{ShareIndex: si, EncryptedShare: es}
	}
	sid, b, err := readChunk(b)
	if err != nil {
		return nil, err
	}
	sig, b, err := readChunk(b)
	if err != nil {
		return nil, err
	}
	if len(b) != 0 {
		return nil, errors.New("threshold: trailing bytes in deal bundle")
	}
	return &dkg.DealBundle{
		DealerIndex: di,
		Deals:       deals,
		Public:      pubs,
		SessionID:   sid,
		Signature:   sig,
	}, nil
}

// MarshalResponseBundle serializes a Round-2 response bundle (no curve points —
// just the per-dealer OK/complaint statuses).
func MarshalResponseBundle(r *dkg.ResponseBundle) ([]byte, error) {
	if r == nil {
		return nil, errors.New("threshold: nil response bundle")
	}
	var buf []byte
	buf = appendUint32(buf, r.ShareIndex)
	buf = appendUint32(buf, uint32(len(r.Responses)))
	for i := range r.Responses {
		buf = appendUint32(buf, r.Responses[i].DealerIndex)
		var status byte
		if r.Responses[i].Status {
			status = 1
		}
		buf = append(buf, status)
	}
	buf = appendChunk(buf, r.SessionID)
	buf = appendChunk(buf, r.Signature)
	return buf, nil
}

// UnmarshalResponseBundle reverses MarshalResponseBundle.
func UnmarshalResponseBundle(b []byte) (*dkg.ResponseBundle, error) {
	si, b, err := readUint32(b)
	if err != nil {
		return nil, err
	}
	nResp, b, err := readUint32(b)
	if err != nil {
		return nil, err
	}
	resps := make([]dkg.Response, nResp)
	for i := uint32(0); i < nResp; i++ {
		var di uint32
		di, b, err = readUint32(b)
		if err != nil {
			return nil, err
		}
		if len(b) < 1 {
			return nil, errors.New("threshold: truncated response status")
		}
		resps[i] = dkg.Response{DealerIndex: di, Status: b[0] == 1}
		b = b[1:]
	}
	sid, b, err := readChunk(b)
	if err != nil {
		return nil, err
	}
	sig, b, err := readChunk(b)
	if err != nil {
		return nil, err
	}
	if len(b) != 0 {
		return nil, errors.New("threshold: trailing bytes in response bundle")
	}
	return &dkg.ResponseBundle{
		ShareIndex: si,
		Responses:  resps,
		SessionID:  sid,
		Signature:  sig,
	}, nil
}

// MarshalCommits serializes the epoch public verification polynomial (the G2
// coefficients an EpochKeyMaterial publishes as its confirmed key) to the wire
// form x/epoch stores and counts agreement over. commits[0] is the epoch master
// public key.
func MarshalCommits(commits []kyber.Point) ([][]byte, error) {
	out := make([][]byte, len(commits))
	for i, c := range commits {
		b, err := c.MarshalBinary()
		if err != nil {
			return nil, err
		}
		out[i] = b
	}
	return out, nil
}

// --- uint32 wire helpers (paired with threshold.go appendChunk/readChunk) ---

func appendUint32(buf []byte, v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return append(buf, b[:]...)
}

func readUint32(b []byte) (v uint32, rest []byte, err error) {
	if len(b) < 4 {
		return 0, nil, errors.New("threshold: truncated uint32")
	}
	return binary.BigEndian.Uint32(b[:4]), b[4:], nil
}
