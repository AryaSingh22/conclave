package types

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// x/escrow operates on compile-time constants rather than a stored Params
// record: the escrow genesis proto carries no Params message and the tx proto
// has no MsgUpdateParams, so escrow policy is not governance-gated in Phase 2
// (D-073). The per-escrow timers (HASHLOCK timeout_blocks, OPTIMISTIC
// dispute_window_blocks) are buyer/seller-chosen via the DeliveryCondition and
// are NOT params; these constants are the protocol-wide knobs.

const (
	// SellerBondBps is the OPTIMISTIC dispute bond the seller (payee) posts at
	// escrow creation, expressed in basis points of the escrowed payment
	// (D-073). It is slashed in full to the buyer (payer) on a lost dispute
	// and returned to the seller on release. 1000 bps = 10%.
	SellerBondBps int64 = 1000

	// ZkDeliveryTimeoutBlocks is the protocol-wide auto-refund window for a
	// ZK_DELIVERY escrow (D-076, task 2.1b): if no valid groth16 proof is
	// submitted within this many blocks of creation, the EndBlock sweep refunds
	// the buyer (ACTIVE→REFUNDED, PHASE2_DESIGN §2.1 criterion 4). Unlike
	// HASHLOCK's timeout_blocks and OPTIMISTIC's dispute_window_blocks (carried
	// per-escrow in the condition), ZkDeliveryCondition has no timeout field, so
	// the window is a protocol constant rather than a party-chosen value — the
	// 2.1b scope-minimizing choice (no proto change, gnark is the single hard
	// thing; mirrors 2.1a's side-record move, D-073). A per-condition ZK timeout
	// (proto field) is a recorded forward item. ~1 day at 6s blocks.
	ZkDeliveryTimeoutBlocks uint64 = 14400
)

// SellerBond returns the OPTIMISTIC dispute bond for one payment amount:
// floor(payment * SellerBondBps / 10000), math.Int only (no floats, §11.6).
func SellerBond(payment math.Int) math.Int {
	return payment.MulRaw(SellerBondBps).QuoRaw(10000)
}

// SellerBondCoins returns the per-denom OPTIMISTIC dispute bond for an
// escrowed payment: SellerBondBps basis points of each coin, rounded down,
// zero-amount coins dropped. Deterministic and order-stable (sdk.Coins are
// sorted), so the bond posted at creation equals the bond returned/slashed at
// resolution.
func SellerBondCoins(payment sdk.Coins) sdk.Coins {
	bond := sdk.NewCoins()
	for _, c := range payment {
		amt := SellerBond(c.Amount)
		if amt.IsPositive() {
			bond = bond.Add(sdk.NewCoin(c.Denom, amt))
		}
	}
	return bond
}
