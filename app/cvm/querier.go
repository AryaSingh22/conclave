package cvm

import (
	"bytes"
	"encoding/json"

	errorsmod "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmvmtypes "github.com/CosmWasm/wasmvm/v2/types"

	escrowkeeper "github.com/AryaSingh22/conclave/x/escrow/keeper"
	sessionkeeper "github.com/AryaSingh22/conclave/x/session/keeper"
)

// QueryPlugins returns the CVM read surface: bounded point lookups over
// sessions, receipts, and escrows — the read-only face of CONCLUDE (task
// 2.2: contracts observe settlement, they never author it). Merged over
// wasmd's defaults, which already REJECT Stargate and gRPC queries, so the
// custom route below is the only Conclave-state read a contract gets.
//
// Every branch is a single-record read (no iteration a contract can widen),
// deterministic (proto-JSON over committed state via the app codec), and
// metered by the calling contract's gas meter through the underlying store
// reads.
func QueryPlugins(cdc codec.Codec, session *sessionkeeper.Keeper, escrow escrowkeeper.Keeper) *wasmkeeper.QueryPlugins {
	return &wasmkeeper.QueryPlugins{
		Custom: customQuerier(cdc, session, escrow),
	}
}

// customQuery is the strict CVM query schema: exactly one branch, no unknown
// fields.
type customQuery struct {
	Session *sessionQuery `json:"session,omitempty"`
	Receipt *receiptQuery `json:"receipt,omitempty"`
	Escrow  *escrowQuery  `json:"escrow,omitempty"`
}

type sessionQuery struct {
	ChamberID uint32 `json:"chamber_id"`
	SessionID uint64 `json:"session_id"`
}

type receiptQuery struct {
	ReceiptID string `json:"receipt_id"`
}

type escrowQuery struct {
	ChamberID uint32 `json:"chamber_id"`
	EscrowID  string `json:"escrow_id"`
}

func customQuerier(cdc codec.Codec, session *sessionkeeper.Keeper, escrow escrowkeeper.Keeper) wasmkeeper.CustomQuerier {
	return func(ctx sdk.Context, request json.RawMessage) ([]byte, error) {
		dec := json.NewDecoder(bytes.NewReader(request))
		dec.DisallowUnknownFields()
		var q customQuery
		if err := dec.Decode(&q); err != nil {
			return nil, wasmvmtypes.UnsupportedRequest{Kind: "CVM custom query: " + err.Error()}
		}

		switch {
		case q.Session != nil && q.Receipt == nil && q.Escrow == nil:
			sess, err := session.GetSession(ctx, q.Session.ChamberID, q.Session.SessionID)
			if err != nil {
				return nil, err
			}
			return cdc.MarshalJSON(&sess)

		case q.Receipt != nil && q.Session == nil && q.Escrow == nil:
			rec, found, err := session.GetReceiptByID(ctx, q.Receipt.ReceiptID)
			if err != nil {
				return nil, err
			}
			if !found {
				return nil, errorsmod.Wrapf(sdkerrors.ErrKeyNotFound, "receipt %s", q.Receipt.ReceiptID)
			}
			return cdc.MarshalJSON(&rec)

		case q.Escrow != nil && q.Session == nil && q.Receipt == nil:
			esc, err := escrow.GetEscrow(ctx, q.Escrow.ChamberID, q.Escrow.EscrowID)
			if err != nil {
				return nil, err
			}
			return cdc.MarshalJSON(&esc)

		default:
			return nil, wasmvmtypes.UnsupportedRequest{Kind: "CVM custom query: exactly one of session/receipt/escrow"}
		}
	}
}
