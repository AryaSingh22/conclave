package unordered

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storetypes "cosmossdk.io/store/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/AryaSingh22/conclave/app/unordered/types"
)

type fixture struct {
	ctx     sdk.Context
	manager Manager
	encCfg  moduletestutil.TestEncodingConfig
}

func setup(t testing.TB) *fixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	encCfg := moduletestutil.MakeTestEncodingConfig()
	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	types.RegisterInterfaces(encCfg.InterfaceRegistry)

	ctx := testCtx.Ctx.WithBlockHeight(10).WithBlockTime(time.Unix(1_750_000_000, 0).UTC())
	return &fixture{
		ctx:     ctx,
		manager: NewManager(runtime.NewKVStoreService(key)),
		encCfg:  encCfg,
	}
}

// buildTx builds a tx with an optional unordered extension; rawExt overrides
// the well-formed extension with arbitrary bytes (malformed-ext tests).
func (f *fixture) buildTx(t testing.TB, timeout *time.Time, rawExt []byte) (sdk.Tx, []byte) {
	t.Helper()
	b := f.encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, b.SetMsgs(&banktypes.MsgSend{}))

	extBuilder, ok := b.(interface{ SetExtensionOptions(...*codectypes.Any) })
	require.True(t, ok)
	switch {
	case rawExt != nil:
		extBuilder.SetExtensionOptions(&codectypes.Any{TypeUrl: types.ExtensionTypeURL, Value: rawExt})
	case timeout != nil:
		ext, err := codectypes.NewAnyWithValue(&types.UnorderedTxExtension{TimeoutTimestamp: *timeout})
		require.NoError(t, err)
		extBuilder.SetExtensionOptions(ext)
	}

	tx := b.GetTx()
	bz, err := f.encCfg.TxConfig.TxEncoder()(tx)
	require.NoError(t, err)
	return tx, bz
}

var nextOK sdk.AnteHandler = func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}

func (f *fixture) run(t testing.TB, tx sdk.Tx, bz []byte, checkTx bool) error {
	t.Helper()
	d := NewUnorderedTxDecorator(f.manager)
	ctx := f.ctx.WithTxBytes(bz).WithIsCheckTx(checkTx)
	_, err := d.AnteHandle(ctx, tx, false, nextOK)
	return err
}

func TestTimeoutFromTx(t *testing.T) {
	f := setup(t)

	tx, _ := f.buildTx(t, nil, nil)
	_, unordered, err := types.TimeoutFromTx(tx)
	require.NoError(t, err)
	require.False(t, unordered, "no extension means ordered")

	want := f.ctx.BlockTime().Add(time.Minute)
	tx, _ = f.buildTx(t, &want, nil)
	got, unordered, err := types.TimeoutFromTx(tx)
	require.NoError(t, err)
	require.True(t, unordered)
	require.True(t, want.Equal(got))

	// a present-but-malformed extension is a hard error, never silently
	// ordered (it would skip the dedup write and re-enable replay)
	tx, _ = f.buildTx(t, nil, []byte{0xff, 0x01, 0x02})
	_, _, err = types.TimeoutFromTx(tx)
	require.ErrorIs(t, err, types.ErrInvalidExt)
}

func TestExtensionOptionChecker(t *testing.T) {
	require.True(t, types.ExtensionOptionChecker(&codectypes.Any{TypeUrl: types.ExtensionTypeURL}))
	require.False(t, types.ExtensionOptionChecker(&codectypes.Any{TypeUrl: "/other.Type"}))
	require.False(t, types.ExtensionOptionChecker(nil))
}

func TestDecorator(t *testing.T) {
	blockTime := time.Unix(1_750_000_000, 0).UTC()

	cases := []struct {
		name    string
		timeout time.Duration // relative to block time
		wantErr error
	}{
		{name: "valid timeout", timeout: 5 * time.Minute},
		{name: "exactly max TTL", timeout: types.MaxUnorderedTTL},
		{name: "beyond max TTL", timeout: types.MaxUnorderedTTL + time.Second, wantErr: types.ErrTTLTooLong},
		{name: "timeout equals block time", timeout: 0, wantErr: types.ErrInvalidTimeout},
		{name: "timeout in the past", timeout: -time.Second, wantErr: types.ErrInvalidTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			timeout := blockTime.Add(tc.timeout)
			tx, bz := f.buildTx(t, &timeout, nil)
			err := f.run(t, tx, bz, false)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}

	t.Run("ordered txs pass through untouched", func(t *testing.T) {
		f := setup(t)
		tx, bz := f.buildTx(t, nil, nil)
		require.NoError(t, f.run(t, tx, bz, false))
		require.NoError(t, f.run(t, tx, bz, false), "ordered txs never enter the dedup cache")
	})

	t.Run("replay rejected across modes (D-028 dual-mode writes)", func(t *testing.T) {
		f := setup(t)
		timeout := blockTime.Add(time.Minute)
		tx, bz := f.buildTx(t, &timeout, nil)
		require.NoError(t, f.run(t, tx, bz, true), "CheckTx records the entry")
		require.ErrorIs(t, f.run(t, tx, bz, false), types.ErrTxReplayed)
		require.ErrorIs(t, f.run(t, tx, bz, true), types.ErrTxReplayed)
	})

	t.Run("malformed extension rejected", func(t *testing.T) {
		f := setup(t)
		tx, bz := f.buildTx(t, nil, []byte{0xff})
		require.ErrorIs(t, f.run(t, tx, bz, false), types.ErrInvalidExt)
	})
}

func TestSweepBoundary(t *testing.T) {
	f := setup(t)
	blockTime := f.ctx.BlockTime()
	hash := sha256.Sum256([]byte("tx"))

	// entries straddling the sweep boundary
	atBlock := blockTime                        // expiry == block time → swept
	justAfter := blockTime.Add(time.Nanosecond) // expiry > block time → kept
	later := blockTime.Add(time.Hour)

	for _, expiry := range []time.Time{atBlock, justAfter, later} {
		require.NoError(t, f.manager.Add(f.ctx, expiry, hash[:]))
	}

	swept, err := f.manager.Sweep(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, swept, "only the <= block-time entry is swept")

	seen, err := f.manager.Contains(f.ctx, atBlock, hash[:])
	require.NoError(t, err)
	require.False(t, seen)
	seen, err = f.manager.Contains(f.ctx, justAfter, hash[:])
	require.NoError(t, err)
	require.True(t, seen, "an entry one nanosecond inside its window survives")
	seen, err = f.manager.Contains(f.ctx, later, hash[:])
	require.NoError(t, err)
	require.True(t, seen)

	// the sweep predicate is the exact complement of the decorator's
	// timeout validation: anything sweepable is already undeliverable
	tx, bz := f.buildTx(t, &atBlock, nil)
	require.ErrorIs(t, f.run(t, tx, bz, false), types.ErrInvalidTimeout)
}

func TestDedupKeyShape(t *testing.T) {
	hash := sha256.Sum256([]byte("tx"))
	expiry := time.Unix(1_750_000_000, 42).UTC()

	key := types.DedupKey(expiry, hash[:])
	require.Len(t, key, 1+8+types.TxHashLength)
	require.Equal(t, types.DedupKeyPrefix[0], key[0])

	// keys sort by expiry: the EndBlock sweep is one time-ordered walk
	earlier := types.DedupKey(expiry.Add(-time.Nanosecond), hash[:])
	require.Equal(t, -1, bytes.Compare(earlier, key))

	// the sweep end covers exactly expiry <= block time
	require.Equal(t, -1, bytes.Compare(key, types.DedupSweepEnd(expiry)))
	require.Equal(t, 1, bytes.Compare(key, types.DedupSweepEnd(expiry.Add(-time.Nanosecond))))
}
