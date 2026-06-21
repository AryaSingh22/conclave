package dkgdaemon

import (
	"context"
	"fmt"

	"github.com/drand/kyber/share/dkg"

	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/crypto/threshold"
	epochtypes "github.com/AryaSingh22/conclave/x/epoch/types"
)

// height returns the current chain height from the node's CometBFT RPC client.
func (d *Daemon) height(ctx context.Context) (uint64, error) {
	if d.clientCtx.Client == nil {
		return 0, fmt.Errorf("no node RPC client")
	}
	st, err := d.clientCtx.Client.Status(ctx)
	if err != nil {
		return 0, err
	}
	h := st.SyncInfo.LatestBlockHeight
	if h < 0 {
		return 0, nil
	}
	return uint64(h), nil
}

// broadcast signs and broadcasts a single DKG message tx from this validator's
// account (sync mode). Sequence/account-number are filled fresh from chain each
// call (txf.Prepare inside BroadcastTx); the DKG submits at most one tx per
// validator per window, spaced over blocks, so sequences don't collide.
func (d *Daemon) broadcast(ctx context.Context, msg sdk.Msg) error {
	txf := tx.Factory{}.
		WithTxConfig(d.clientCtx.TxConfig).
		WithAccountRetriever(d.clientCtx.AccountRetriever).
		WithChainID(d.clientCtx.ChainID).
		WithKeybase(d.clientCtx.Keyring).
		WithGas(600000).
		WithGasAdjustment(1.3).
		WithFees(""). // zero-fee dev chain (min-gas-prices 0uclv)
		WithSimulateAndExecute(false)

	clientCtx := d.clientCtx.WithCmdContext(ctx)
	return tx.BroadcastTx(clientCtx, txf, msg)
}

// --- transcript decoders (read peers' opaque bundles from the chain) ---

func (d *Daemon) deals(ctx context.Context, q epochtypes.QueryClient, epochID uint64) ([]*dkg.DealBundle, error) {
	resp, err := q.DKGTranscript(ctx, &epochtypes.QueryDKGTranscriptRequest{EpochId: epochID, Round: 0})
	if err != nil {
		return nil, err
	}
	out := make([]*dkg.DealBundle, 0, len(resp.Bundles))
	for _, b := range resp.Bundles {
		dl, err := threshold.UnmarshalDealBundle(b.Bundle)
		if err != nil {
			d.logger.Error("decode deal bundle", "index", b.DkgIndex, "err", err)
			continue
		}
		out = append(out, dl)
	}
	return out, nil
}

func (d *Daemon) responses(ctx context.Context, q epochtypes.QueryClient, epochID uint64) ([]*dkg.ResponseBundle, error) {
	resp, err := q.DKGTranscript(ctx, &epochtypes.QueryDKGTranscriptRequest{EpochId: epochID, Round: 1})
	if err != nil {
		return nil, err
	}
	out := make([]*dkg.ResponseBundle, 0, len(resp.Bundles))
	for _, b := range resp.Bundles {
		r, err := threshold.UnmarshalResponseBundle(b.Bundle)
		if err != nil {
			d.logger.Error("decode response bundle", "index", b.DkgIndex, "err", err)
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (d *Daemon) justifications(ctx context.Context, q epochtypes.QueryClient, epochID uint64) ([]*dkg.JustificationBundle, error) {
	resp, err := q.DKGTranscript(ctx, &epochtypes.QueryDKGTranscriptRequest{EpochId: epochID, Round: 2})
	if err != nil {
		return nil, err
	}
	out := make([]*dkg.JustificationBundle, 0, len(resp.Bundles))
	for _, b := range resp.Bundles {
		j, err := threshold.UnmarshalJustificationBundle(b.Bundle)
		if err != nil {
			d.logger.Error("decode justification bundle", "index", b.DkgIndex, "err", err)
			continue
		}
		out = append(out, j)
	}
	return out, nil
}
