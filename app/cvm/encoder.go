// Package cvm implements the CVM custom bindings — SPEC §0's name for
// Conclave's CosmWasm contract runtime surface (task 2.2, D-078; the seam
// recorded at x/session tx.pb.go "MsgConclude … exposed to CVM via custom
// bindings", re-examined and resolved as READ-ONLY).
//
// The authority model (the task-2.2 plan-stop decision), three layers deep:
//
//  1. ALLOWLIST — the custom-message encoder below knows exactly three escrow
//     msgs (release / prove-delivery / dispute) and nothing else. There is no
//     Conclude encoding: settlement authorship (the §6.4 all-or-nothing
//     graph) stays module-authority-only, in EndBlock Half-B and the
//     authority-gated MsgConclude. The contract-actionable pieces of
//     settlement ARE the escrow legs, each independently gated by the 2.1
//     keeper rules (knowledge-gated release/proof, payer-identity-gated
//     dispute).
//  2. NO PASSTHROUGH — the Any/Stargate encoder is replaced with a rejection,
//     so a contract cannot bypass the allowlist to reach MsgConclude (or any
//     other native msg). Stargate/gRPC QUERIES are already rejected by
//     wasmd's defaults.
//  3. SIGNER STAMPING — every encoded msg carries the CALLING CONTRACT's own
//     address as signer. The schema has no signer field (unknown fields are
//     rejected), and wasmd's SDK message handler independently verifies that
//     every signer of a dispatched msg IS the contract. A contract acts only
//     as itself: even if a passthrough leaked, a forged MsgConclude would
//     need authority == contract (signer check) AND authority == gov module
//     (keeper check) — structurally impossible.
//
// Gas: contract execution burns wasm gas through wasmd's GasRegister
// (wasm↔SDK conversion at the pinned default multiplier); a CVM→native call
// dispatches on the SAME ctx gas meter, so native escrow work costs SDK gas
// exactly as a direct tx would. CVM queries likewise run under the calling
// contract's gas meter via the store reads they perform.
package cvm

import (
	"bytes"
	"encoding/json"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmvmtypes "github.com/CosmWasm/wasmvm/v2/types"

	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
)

// MessageEncoders returns the CVM write surface. Merged over wasmd's
// defaults, it REPLACES the Custom route (the escrow allowlist) and the Any
// route (rejected). CosmWasm-native routes (Bank/Staking/Distribution/IBC/
// Wasm/Gov) keep their defaults — they are contract-own-funds operations,
// signer-validated by the same dispatch check as layer 3.
func MessageEncoders() *wasmkeeper.MessageEncoders {
	return &wasmkeeper.MessageEncoders{
		Custom: EncodeCustomMsg,
		Any:    rejectAnyMsg,
	}
}

// customMsg is the strict CVM custom-message schema: exactly one branch set,
// no unknown fields anywhere (DisallowUnknownFields), and — deliberately —
// no signer field on any branch.
type customMsg struct {
	ReleaseEscrow       *releaseEscrowMsg       `json:"release_escrow,omitempty"`
	SubmitDeliveryProof *submitDeliveryProofMsg `json:"submit_delivery_proof,omitempty"`
	RaiseDispute        *raiseDisputeMsg        `json:"raise_dispute,omitempty"`
}

type releaseEscrowMsg struct {
	EscrowID string `json:"escrow_id"`
	// Preimage is base64 in JSON (Go []byte convention, matching CosmWasm
	// Binary). HASHLOCK release and OPTIMISTIC reveal are knowledge-gated
	// (D-073): presenting the preimage IS the authorization.
	Preimage []byte `json:"preimage"`
}

type submitDeliveryProofMsg struct {
	EscrowID string `json:"escrow_id"`
	// Proof is the groth16 proof; release is proof-gated and instance-bound
	// (D-076) — the verifier reconstructs the public witness from the escrow
	// record, never from the submitter.
	Proof []byte `json:"proof"`
}

type raiseDisputeMsg struct {
	EscrowID string `json:"escrow_id"`
	// no further fields: the dispute is payer-identity-gated in the keeper —
	// the signer (the contract) must BE the escrow's payer-sigil controller.
}

// EncodeCustomMsg translates a contract's custom msg into native escrow msgs.
// The sender — the calling contract's address, supplied by wasmd, never by
// the contract's JSON — is stamped as the signer of every encoded msg.
func EncodeCustomMsg(sender sdk.AccAddress, raw json.RawMessage) ([]sdk.Msg, error) {
	// Rejections deliberately do NOT wrap wasmd's ErrUnknownMsg: the message
	// handler chain treats that as "try the next handler" and would swallow
	// this encoder's provenance in a generic no-handler error. An allowlist
	// miss is a hard, attributable failure.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var msg customMsg
	if err := dec.Decode(&msg); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "CVM custom msg (allowlist): %s", err)
	}

	var msgs []sdk.Msg
	if msg.ReleaseEscrow != nil {
		msgs = append(msgs, &escrowtypes.MsgReleaseEscrow{
			Signer:   sender.String(),
			EscrowId: msg.ReleaseEscrow.EscrowID,
			Preimage: msg.ReleaseEscrow.Preimage,
		})
	}
	if msg.SubmitDeliveryProof != nil {
		msgs = append(msgs, &escrowtypes.MsgSubmitDeliveryProof{
			Signer:   sender.String(),
			EscrowId: msg.SubmitDeliveryProof.EscrowID,
			Proof:    msg.SubmitDeliveryProof.Proof,
		})
	}
	if msg.RaiseDispute != nil {
		msgs = append(msgs, &escrowtypes.MsgRaiseDispute{
			Signer:   sender.String(),
			EscrowId: msg.RaiseDispute.EscrowID,
		})
	}

	if len(msgs) != 1 {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
			"CVM custom msg (allowlist): exactly one of release_escrow/submit_delivery_proof/raise_dispute, got %d", len(msgs))
	}
	return msgs, nil
}

// rejectAnyMsg closes the generic protobuf passthrough (layer 2): the
// allowlist above is the only route from a contract into native msgs.
func rejectAnyMsg(_ sdk.AccAddress, _ *wasmvmtypes.AnyMsg) ([]sdk.Msg, error) {
	return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized,
		"CVM: any/stargate passthrough is disabled; use the custom-msg allowlist")
}
