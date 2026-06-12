package unordered

import (
	"crypto/sha256"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/app/unordered/types"
)

// UnorderedTxDecorator enforces SPEC §6.7 for txs carrying the unordered
// extension: timeout_timestamp must be in the future, within the 10-minute
// TTL cap, and the tx hash must not already sit in the dedup cache. It
// runs after signature verification (dedup state is only ever written for
// fully verified txs) and before MandateDecorator (unordered txs flow
// through window charging with no special-casing — that combination is
// what bounds N parallel unordered txs from one stolen key).
//
// Ordered txs (no extension) pass through untouched.
type UnorderedTxDecorator struct {
	manager Manager
}

// NewUnorderedTxDecorator creates the SPEC §6.7 dedup decorator.
func NewUnorderedTxDecorator(manager Manager) UnorderedTxDecorator {
	return UnorderedTxDecorator{manager: manager}
}

var _ sdk.AnteDecorator = UnorderedTxDecorator{}

// AnteHandle implements SPEC §6.7 unordered-tx validation and recording.
func (d UnorderedTxDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	timeout, unordered, err := types.TimeoutFromTx(tx)
	if err != nil {
		return ctx, err
	}
	if !unordered {
		return next(ctx, tx, simulate)
	}

	blockTime := ctx.BlockTime()
	if !timeout.After(blockTime) {
		return ctx, errorsmod.Wrapf(types.ErrInvalidTimeout,
			"timeout %s, block time %s", timeout, blockTime)
	}
	if timeout.Sub(blockTime) > types.MaxUnorderedTTL {
		return ctx, errorsmod.Wrapf(types.ErrTTLTooLong,
			"timeout %s exceeds block time %s + %s", timeout, blockTime, types.MaxUnorderedTTL)
	}

	txHash := sha256.Sum256(ctx.TxBytes())
	seen, err := d.manager.Contains(ctx, timeout, txHash[:])
	if err != nil {
		return ctx, err
	}
	if seen {
		return ctx, errorsmod.Wrapf(types.ErrTxReplayed, "tx %X", txHash)
	}
	// record in CheckTx AND DeliverTx (D-028): the check state bounds
	// mempool replays before a block is ever proposed; the deliver state
	// is the consensus guarantee
	if err := d.manager.Add(ctx, timeout, txHash[:]); err != nil {
		return ctx, err
	}

	return next(ctx, tx, simulate)
}
