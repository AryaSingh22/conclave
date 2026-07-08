package cvm

import (
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
)

var contractAddr = sdk.AccAddress([]byte("cvm-test-contract-addr----------"))

// TestEncode_AllowlistShapes pins the entire write surface: each of the three
// allowlisted branches encodes to exactly its native escrow msg, with the
// CALLING CONTRACT stamped as signer.
func TestEncode_AllowlistShapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want func(t *testing.T, msg sdk.Msg)
	}{
		{
			name: "release_escrow",
			in:   `{"release_escrow":{"escrow_id":"ab12","preimage":"cHJlaW1hZ2U="}}`,
			want: func(t *testing.T, msg sdk.Msg) {
				m, ok := msg.(*escrowtypes.MsgReleaseEscrow)
				if !ok {
					t.Fatalf("got %T, want MsgReleaseEscrow", msg)
				}
				if m.Signer != contractAddr.String() {
					t.Fatalf("signer %q is not the contract", m.Signer)
				}
				if m.EscrowId != "ab12" || string(m.Preimage) != "preimage" {
					t.Fatalf("fields not mapped: %+v", m)
				}
			},
		},
		{
			name: "submit_delivery_proof",
			in:   `{"submit_delivery_proof":{"escrow_id":"cd34","proof":"cHJvb2Y="}}`,
			want: func(t *testing.T, msg sdk.Msg) {
				m, ok := msg.(*escrowtypes.MsgSubmitDeliveryProof)
				if !ok {
					t.Fatalf("got %T, want MsgSubmitDeliveryProof", msg)
				}
				if m.Signer != contractAddr.String() {
					t.Fatalf("signer %q is not the contract", m.Signer)
				}
				if m.EscrowId != "cd34" || string(m.Proof) != "proof" {
					t.Fatalf("fields not mapped: %+v", m)
				}
			},
		},
		{
			name: "raise_dispute",
			in:   `{"raise_dispute":{"escrow_id":"ef56"}}`,
			want: func(t *testing.T, msg sdk.Msg) {
				m, ok := msg.(*escrowtypes.MsgRaiseDispute)
				if !ok {
					t.Fatalf("got %T, want MsgRaiseDispute", msg)
				}
				if m.Signer != contractAddr.String() {
					t.Fatalf("signer %q is not the contract", m.Signer)
				}
				if m.EscrowId != "ef56" {
					t.Fatalf("fields not mapped: %+v", m)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msgs, err := EncodeCustomMsg(contractAddr, json.RawMessage(tc.in))
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if len(msgs) != 1 {
				t.Fatalf("got %d msgs, want 1", len(msgs))
			}
			tc.want(t, msgs[0])
		})
	}
}

// TestEncode_ConcludeUnreachable is the layer-1 forge gate: there is no
// custom-msg spelling of CONCLUDE. If someone adds a "conclude" branch to the
// allowlist, this goes red.
func TestEncode_ConcludeUnreachable(t *testing.T) {
	for _, in := range []string{
		`{"conclude":{"session_id":1}}`,
		`{"msg_conclude":{"session_id":1}}`,
		`{"session_conclude":{}}`,
	} {
		if _, err := EncodeCustomMsg(contractAddr, json.RawMessage(in)); err == nil {
			t.Fatalf("custom msg %s encoded — CONCLUDE must be unreachable from contracts", in)
		}
	}
}

// TestEncode_SignerCannotBeNamed is the layer-3 stamping gate: the schema has
// no signer field, so a contract cannot act as anyone but itself. If a signer
// field is ever added to a branch, this goes red.
func TestEncode_SignerCannotBeNamed(t *testing.T) {
	in := `{"release_escrow":{"escrow_id":"ab12","preimage":"cHJl","signer":"conc1attacker"}}`
	if _, err := EncodeCustomMsg(contractAddr, json.RawMessage(in)); err == nil {
		t.Fatal("a custom msg naming its own signer must be rejected")
	}
}

// TestEncode_ExactlyOneBranch: empty and multi-branch msgs are ambiguous and
// rejected.
func TestEncode_ExactlyOneBranch(t *testing.T) {
	for _, in := range []string{
		`{}`,
		`{"release_escrow":{"escrow_id":"a"},"raise_dispute":{"escrow_id":"b"}}`,
	} {
		if _, err := EncodeCustomMsg(contractAddr, json.RawMessage(in)); err == nil {
			t.Fatalf("custom msg %s must be rejected (exactly one branch)", in)
		}
	}
}

// TestEncode_AnyPassthroughRejected is the layer-2 gate: the Any/Stargate
// route into arbitrary native msgs is closed. If the Any encoder is ever
// restored to wasmd's default, this goes red.
func TestEncode_AnyPassthroughRejected(t *testing.T) {
	enc := MessageEncoders()
	if enc.Any == nil {
		t.Fatal("Any encoder must be explicitly set (to a rejection), not left to the wasmd default")
	}
	_, err := enc.Any(contractAddr, nil)
	if err == nil {
		t.Fatal("Any passthrough must be rejected")
	}
	if !strings.Contains(err.Error(), "passthrough is disabled") {
		t.Fatalf("rejection must come from the CVM allowlist, got: %v", err)
	}
}
