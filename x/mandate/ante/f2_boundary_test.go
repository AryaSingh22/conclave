package ante

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"

	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
)

// TestSpendIntentF2BoundaryPin PINS the AUDIT F-2 boundary as inherited by
// task 2.5: spendIntent values ONLY MsgSend/MsgMultiSend/MsgSubmitIntent;
// every other balance-moving msg — IBC MsgTransfer here, now carrying REAL
// value since 2.5 brought the ibc/<hash> USDC voucher — contributes ZERO
// spend and is gated only by the step-3 allowlist. That is the recorded,
// bounded exposure (a controller who allowlists MsgTransfer gets no window
// ceiling on it). Phase-3 task 3.4 owns closing it; when 3.4 extends the
// valuator, THIS test is the one it must consciously flip — the boundary
// moves by a red test, never by drift.
func TestSpendIntentF2BoundaryPin(t *testing.T) {
	f := setup(t)
	d := NewMandateDecorator(f.k)

	transfer := ibctransfertypes.NewMsgTransfer(
		"transfer", "channel-0",
		sdk.NewCoin("ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2", math.NewInt(1_000_000_000)),
		f.grantee.String(), "cosmos1remote", clienttypes.NewHeight(1, 1000), 0, "",
	)

	commit, settled := d.spendIntent(f.ctx, transfer)
	require.True(t, commit.IsZero(),
		"F-2 boundary: MsgTransfer must contribute zero COMMIT spend (3.4 flips this deliberately)")
	require.True(t, settled.IsZero(),
		"F-2 boundary: MsgTransfer must contribute zero SETTLED spend (3.4 flips this deliberately)")
}
