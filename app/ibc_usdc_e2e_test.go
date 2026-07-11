package app_test

// Task 2.5 IBC-USDC e2e: two in-process Conclave chains, a REAL ICS-20
// transfer channel, USDC minted on the counterparty (the Noble stand-in) and
// transferred across — then the resulting ibc/<hash> voucher driven through
// every 2.5 surface ON THE LIVE CHAIN STORE: FaceValuePriceSource face value
// (the F-3 boundary carried exactly — data legs still zero, 3.4 owns the
// swap), mandate ceiling drawdown in the voucher denom, and fee abstraction
// cold (fixed fallback) → warm (session TWAP) through the APP-WIRED seam.
// The LIVE two-binary localnet + relayer demo is the §6 exit demo's scope
// (recorded at the 2.5 plan stop, not a deviation).

import (
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	sims "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"

	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"

	"github.com/AryaSingh22/conclave/app"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

func setupConclaveTestingApp() (ibctesting.TestingApp, map[string]json.RawMessage) {
	conclave := app.NewConclaveApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true, sims.EmptyAppOptions{})
	return conclave, conclave.DefaultGenesis()
}

func TestIBCUSDCVoucherEndToEnd(t *testing.T) {
	ibctesting.DefaultTestingAppInit = setupConclaveTestingApp

	coord := ibctesting.NewCoordinator(t, 2)
	chainA := coord.GetChain(ibctesting.GetChainID(1)) // Conclave, receiving real USDC
	chainB := coord.GetChain(ibctesting.GetChainID(2)) // issuer counterparty (Noble role)

	path := ibctesting.NewPath(chainA, chainB)
	path.EndpointA.ChannelConfig.PortID = ibctesting.TransferPort
	path.EndpointB.ChannelConfig.PortID = ibctesting.TransferPort
	path.EndpointA.ChannelConfig.Version = transfertypes.Version
	path.EndpointB.ChannelConfig.Version = transfertypes.Version
	coord.Setup(path)

	appA := chainA.App.(*app.ConclaveApp)
	appB := chainB.App.(*app.ConclaveApp)

	// mint USDC on the issuer chain and fund its sender
	const usdc = "uusdc"
	amount := sdk.NewCoin(usdc, math.NewInt(1_000_000_000)) // 1000 USDC
	ctxB := chainB.GetContext()
	require.NoError(t, appB.BankKeeper.MintCoins(ctxB, minttypes.ModuleName, sdk.NewCoins(amount)))
	require.NoError(t, appB.BankKeeper.SendCoinsFromModuleToAccount(ctxB, minttypes.ModuleName, chainB.SenderAccount.GetAddress(), sdk.NewCoins(amount)))

	// transfer B → A over the real channel
	msg := transfertypes.NewMsgTransfer(
		path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID,
		amount, chainB.SenderAccount.GetAddress().String(), chainA.SenderAccount.GetAddress().String(),
		chainA.GetTimeoutHeight(), 0, "",
	)
	res, err := chainB.SendMsgs(msg)
	require.NoError(t, err)
	packet, err := ibctesting.ParsePacketFromEvents(res.Events)
	require.NoError(t, err)
	require.NoError(t, path.RelayPacket(packet))

	// the ibc/<hash> voucher landed on Conclave
	voucherDenom := transfertypes.ParseDenomTrace(
		transfertypes.GetPrefixedDenom(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, usdc),
	).IBCDenom()
	require.True(t, strings.HasPrefix(voucherDenom, "ibc/"))
	ctxA := chainA.GetContext()
	sender := chainA.SenderAccount.GetAddress()
	require.Equal(t, amount.Amount, appA.BankKeeper.GetBalance(ctxA, sender, voucherDenom).Amount,
		"the full transferred amount arrives as the ibc/<hash> voucher")

	// --- (1) F-3 boundary carried exactly: FaceValuePriceSource values the
	// voucher COIN leg at face in its own denom; data legs still value zero.
	voucherCoin := sdk.NewCoin(voucherDenom, math.NewInt(250))
	coinLeg := veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Coin{Coin: &voucherCoin}}
	require.Equal(t, sdk.NewCoins(voucherCoin), mandatetypes.FaceValuePriceSource{}.GiveLegValue(ctxA, &coinLeg),
		"a real-USDC coin leg values at face through the EXISTING price source")
	dataLeg := veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Data{Data: &veiltypes.DataGrant{ContentHash: []byte{1}}}}
	require.Nil(t, mandatetypes.FaceValuePriceSource{}.GiveLegValue(ctxA, &dataLeg),
		"F-3: data legs still value zero — the boundary is inherited, not moved")

	// --- (2) mandate ceilings in the voucher denom draw down and bind
	docHash := sha256.Sum256([]byte("ibc-e2e-sigil"))
	sigilID, err := appA.SigilKeeper.RegisterSigil(ctxA, sender, docHash[:], "ipfs://meta", nil, 0, 0)
	require.NoError(t, err)
	grantee := sdk.AccAddress([]byte("ibc_e2e_grantee_____"))
	terms := mandatetypes.MandateTerms{
		SigilId:            sigilID,
		GranteeAddr:        grantee.String(),
		AllowedMsgTypes:    []string{"/conclave.veil.v1.MsgSubmitIntent"},
		SpendCeilingPerTx:  sdk.NewCoins(sdk.NewCoin(voucherDenom, math.NewInt(500))),
		SpendCeilingWindow: sdk.NewCoins(sdk.NewCoin(voucherDenom, math.NewInt(500))),
		WindowBlocks:       86_400,
		FeeDenoms:          []string{voucherDenom},
		ExpiryHeight:       1_000_000,
	}
	_, err = appA.MandateKeeper.IssueMandate(ctxA, sender, terms)
	require.NoError(t, err)
	m, found, err := appA.MandateKeeper.ActiveMandateByGrantee(ctxA, 0, grantee)
	require.NoError(t, err)
	require.True(t, found)

	require.NoError(t, appA.MandateKeeper.ChargeWindow(ctxA, m,
		sdk.NewCoins(), sdk.NewCoins(sdk.NewCoin(voucherDenom, math.NewInt(400)))),
		"voucher-denom settled spend under the ceiling charges the window")
	m, _, err = appA.MandateKeeper.ActiveMandateByGrantee(ctxA, 0, grantee)
	require.NoError(t, err)
	require.ErrorIs(t, appA.MandateKeeper.ChargeWindow(ctxA, m,
		sdk.NewCoins(), sdk.NewCoins(sdk.NewCoin(voucherDenom, math.NewInt(200)))),
		mandatetypes.ErrMandateExceeded,
		"the ibc/<hash> ceiling BINDS: 400+200 > 500")

	// --- (3) fee abstraction in the voucher denom: cold → fallback, then
	// warm → session TWAP, through the seam NewConclaveApp wired.
	require.NoError(t, appA.BankKeeper.SendCoins(ctxA, sender, grantee,
		sdk.NewCoins(sdk.NewCoin(voucherDenom, math.NewInt(100_000)))))
	poolFloat := sdk.NewCoins(sdk.NewCoin("uclv", math.NewInt(1_000_000)))
	require.NoError(t, appA.BankKeeper.MintCoins(ctxA, minttypes.ModuleName, poolFloat))
	require.NoError(t, appA.BankKeeper.SendCoinsFromModuleToModule(ctxA, minttypes.ModuleName, mandatetypes.ModuleName, poolFloat))

	collectorAddr := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	collectorClv := func() math.Int { return appA.BankKeeper.GetBalance(ctxA, collectorAddr, "uclv").Amount }
	fee := sdk.NewCoins(sdk.NewCoin(voucherDenom, math.NewInt(1_000)))

	// cold start: no clearing-price history for the voucher pair → the fixed
	// fallback rate (dev 1:1) converts
	before := collectorClv()
	require.NoError(t, appA.MandateKeeper.ExecuteFeeAbstraction(ctxA, m, fee))
	require.Equal(t, math.NewInt(1_000), collectorClv().Sub(before),
		"cold start: fixed fallback rate until price history exists")

	// warm up: settle-time observations for uclv quoted in the voucher denom
	// (price 2.0 voucher per uclv), enough to clear twap_min_observations
	sessParams, err := appA.SessionKeeper.GetParams(ctxA)
	require.NoError(t, err)
	for i := uint64(0); i < sessParams.TwapMinObservations; i++ {
		require.NoError(t, appA.SessionKeeper.RecordClearingPrices(ctxA, []sessiontypes.PricePoint{
			{Base: "uclv", Quote: voucherDenom, Price: math.LegacyNewDec(2)},
		}))
	}
	price, warm := appA.SessionKeeper.TwapPrice(ctxA, "uclv", voucherDenom)
	require.True(t, warm)
	require.Equal(t, math.LegacyNewDec(2), price)

	// warm: the SAME call now converts at the TWAP (1000/2 = 500), proving
	// the app-wired TwapSource — not the param — prices a warm fee
	before = collectorClv()
	require.NoError(t, appA.MandateKeeper.ExecuteFeeAbstraction(ctxA, m, fee))
	require.Equal(t, math.NewInt(500), collectorClv().Sub(before),
		"warm: the session TWAP converts the real-USDC fee through the wired seam")
}
