package threshold

// Task 1.5 — validator-side DKG bundle transport serde (D-068). These prove the
// authored wire form round-trips a real kyber DKG: a committee built by
// NewInProcessCommittee, with every Round-1/Round-2 bundle SERIALIZED, routed as
// opaque bytes (as the x/epoch on-chain transport will), and DESERIALIZED before
// the next round, finalizes to the SAME EpochPubKey on every node — and that key
// is the one a freshly-built reveal Scheme decrypts under. This is the crypto
// half of the Task-1.5 acceptance ("DKG completes and the resulting EpochPubKey
// is consistent across nodes"); the chain-state half (windows, agreement
// counting, rotation) lives in x/epoch.

import (
	"bytes"
	"testing"

	"github.com/drand/kyber/share/dkg"
)

// TestDealBundleSerdeRoundTrip proves Marshal∘Unmarshal is the identity on a real
// deal bundle: the reconstructed bundle's Hash() (which the dealer signature is
// over) is byte-identical, so a deserialized bundle still verifies in ProcessDeals.
func TestDealBundleSerdeRoundTrip(t *testing.T) {
	parts, err := NewInProcessCommittee(4, 3)
	if err != nil {
		t.Fatalf("NewInProcessCommittee: %v", err)
	}
	deal, err := parts[0].Round1Deal()
	if err != nil {
		t.Fatalf("Round1Deal: %v", err)
	}
	want := deal.Hash()

	enc, err := MarshalDealBundle(deal)
	if err != nil {
		t.Fatalf("MarshalDealBundle: %v", err)
	}
	got, err := UnmarshalDealBundle(enc)
	if err != nil {
		t.Fatalf("UnmarshalDealBundle: %v", err)
	}
	if got.DealerIndex != deal.DealerIndex {
		t.Fatalf("dealer index %d want %d", got.DealerIndex, deal.DealerIndex)
	}
	if !bytes.Equal(got.Hash(), want) {
		t.Fatalf("deal bundle Hash() differs across serde — signature would not verify")
	}
	// re-encoding the decoded bundle is byte-stable (canonical point encoding).
	enc2, err := MarshalDealBundle(got)
	if err != nil {
		t.Fatalf("re-MarshalDealBundle: %v", err)
	}
	if !bytes.Equal(enc, enc2) {
		t.Fatalf("deal bundle wire form not byte-stable across re-encode")
	}
}

// TestDKGViaTransportConsistentKey runs the full multi-round DKG with every
// bundle passing through the authored serde (the opaque-byte path the on-chain
// transport uses) and asserts all n nodes finalize the SAME EpochPubKey, and that
// key actually decrypts a ciphertext encrypted to it.
func TestDKGViaTransportConsistentKey(t *testing.T) {
	const n, thr = 5, 4
	parts, err := NewInProcessCommittee(n, thr)
	if err != nil {
		t.Fatalf("NewInProcessCommittee: %v", err)
	}

	// Round 1 — each node deals; bundles go out as opaque bytes.
	dealWire := make([][]byte, 0, n)
	for _, p := range parts {
		d, err := p.Round1Deal()
		if err != nil {
			t.Fatalf("Round1Deal: %v", err)
		}
		enc, err := MarshalDealBundle(d)
		if err != nil {
			t.Fatalf("MarshalDealBundle: %v", err)
		}
		dealWire = append(dealWire, enc)
	}

	// Round 2 — each node reads ALL deal bytes from "chain", decodes, responds.
	respWire := make([][]byte, 0, n)
	for _, p := range parts {
		r, err := p.Round2Response(decodeDeals(t, dealWire))
		if err != nil {
			t.Fatalf("Round2Response: %v", err)
		}
		if r == nil {
			continue
		}
		enc, err := MarshalResponseBundle(r)
		if err != nil {
			t.Fatalf("MarshalResponseBundle: %v", err)
		}
		respWire = append(respWire, enc)
	}

	// Round 3 — each node reads ALL response bytes, decodes, finalizes locally.
	mats := make([]*EpochKeyMaterial, 0, n)
	for i, p := range parts {
		mat, err := p.Finalize(decodeResps(t, respWire))
		if err != nil {
			t.Fatalf("Finalize[%d]: %v", i, err)
		}
		if !bytes.Equal(mat.EpochPub, mats0(mats)) && len(mats) > 0 {
			t.Fatalf("node %d EpochPub differs from node 0 — DKG not consistent across transport", i)
		}
		mats = append(mats, mat)
	}

	// the consistent published key actually decrypts a t-of-n reveal.
	sch, err := NewScheme(mats[0].Commits, n)
	if err != nil {
		t.Fatalf("NewScheme: %v", err)
	}
	client := NewClientScheme()
	payload := []byte("epoch key from the transport-routed DKG decrypts")
	ct, err := client.Encrypt(mats[0].EpochPub, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := sch.Combine(sharesFor(t, sch, mats, ct, rangeN(thr)...), ct)
	if err != nil {
		t.Fatalf("Combine: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("recovered %q want %q", got, payload)
	}
}

func mats0(mats []*EpochKeyMaterial) []byte {
	if len(mats) == 0 {
		return nil
	}
	return mats[0].EpochPub
}

func decodeDeals(t *testing.T, wire [][]byte) []*dkg.DealBundle {
	t.Helper()
	out := make([]*dkg.DealBundle, 0, len(wire))
	for _, w := range wire {
		d, err := UnmarshalDealBundle(w)
		if err != nil {
			t.Fatalf("UnmarshalDealBundle: %v", err)
		}
		out = append(out, d)
	}
	return out
}

func decodeResps(t *testing.T, wire [][]byte) []*dkg.ResponseBundle {
	t.Helper()
	out := make([]*dkg.ResponseBundle, 0, len(wire))
	for _, w := range wire {
		r, err := UnmarshalResponseBundle(w)
		if err != nil {
			t.Fatalf("UnmarshalResponseBundle: %v", err)
		}
		out = append(out, r)
	}
	return out
}
