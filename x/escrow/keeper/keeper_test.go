package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	"github.com/AryaSingh22/conclave/x/escrow/types"
)

// ---- mocks ----------------------------------------------------------------

// mockBank tracks per-account/per-module balances so escrow fund flows
// (payment in/out, seller bond in/out, slash) are assertable. Debits use
// SafeSub so an underfunded account fails like x/bank's ErrInsufficientFunds —
// the path the OPTIMISTIC seller-bond ErrInsufficientBond test exercises.
type mockBank struct {
	bal map[string]sdk.Coins
}

func newMockBank() *mockBank { return &mockBank{bal: map[string]sdk.Coins{}} }

func (b *mockBank) get(key string) sdk.Coins {
	if c, ok := b.bal[key]; ok {
		return c
	}
	return sdk.NewCoins()
}

func (b *mockBank) fund(key string, coins sdk.Coins) { b.bal[key] = b.get(key).Add(coins...) }

func (b *mockBank) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error {
	next, neg := b.get(sender.String()).SafeSub(amt...)
	if neg {
		return fmt.Errorf("insufficient funds: %s has %s, needs %s", sender, b.get(sender.String()), amt)
	}
	b.bal[sender.String()] = next
	b.fund(module, amt)
	return nil
}

func (b *mockBank) SendCoinsFromModuleToAccount(_ context.Context, module string, recipient sdk.AccAddress, amt sdk.Coins) error {
	next, neg := b.get(module).SafeSub(amt...)
	if neg {
		return fmt.Errorf("module %s insufficient: has %s, needs %s", module, b.get(module), amt)
	}
	b.bal[module] = next
	b.fund(recipient.String(), amt)
	return nil
}

// mockSigil maps sigil ids to controller addresses and records every
// PenalizeLostDispute call so the OPTIMISTIC lost-dispute reputation hook is
// assertable.
type mockSigil struct {
	addrs     map[string]sdk.AccAddress
	penalized []string
}

func newMockSigil() *mockSigil { return &mockSigil{addrs: map[string]sdk.AccAddress{}} }

func (s *mockSigil) ControllerAddress(_ sdk.Context, _ uint32, sigil string) (sdk.AccAddress, error) {
	a, ok := s.addrs[sigil]
	if !ok {
		return nil, fmt.Errorf("unknown sigil %s", sigil)
	}
	return a, nil
}

func (s *mockSigil) PenalizeLostDispute(_ sdk.Context, _ uint32, sigil string) error {
	s.penalized = append(s.penalized, sigil)
	return nil
}

func (s *mockSigil) penalizedCount(sigil string) int {
	n := 0
	for _, p := range s.penalized {
		if p == sigil {
			n++
		}
	}
	return n
}

// ---- fixture --------------------------------------------------------------

const (
	buyerSigil  = "sigil-buyer"
	sellerSigil = "sigil-seller"
)

type escrowFixture struct {
	k     Keeper
	ctx   sdk.Context
	bank  *mockBank
	sigil *mockSigil
	buyer sdk.AccAddress
	selr  sdk.AccAddress
}

func setupEscrow(t testing.TB) *escrowFixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{types.StoreKey: key}, nil, nil,
	).WithBlockHeight(100)
	encCfg := moduletestutil.MakeTestEncodingConfig()

	bank := newMockBank()
	sigil := newMockSigil()
	buyer := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	selr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	sigil.addrs[buyerSigil] = buyer
	sigil.addrs[sellerSigil] = selr

	k := NewKeeper(encCfg.Codec, runtime.NewKVStoreService(key), bank, sigil)
	return &escrowFixture{k: k, ctx: ctx, bank: bank, sigil: sigil, buyer: buyer, selr: selr}
}

// testID renders a deterministic 32-byte hex escrow id from a seed.
func testID(seed byte) string {
	b := make([]byte, types.IDLength)
	for i := range b {
		b[i] = seed
	}
	return hex.EncodeToString(b)
}

func coins(denom string, amt int64) sdk.Coins { return sdk.NewCoins(sdk.NewInt64Coin(denom, amt)) }

// newHashlockEscrow assembles (does not store) a HASHLOCK escrow record.
func newHashlockEscrow(id string, payloadHash []byte, timeoutBlocks uint64, amount sdk.Coins) types.Escrow {
	return types.Escrow{
		Id:         id,
		PayerSigil: buyerSigil,
		PayeeSigil: sellerSigil,
		Amount:     amount,
		Condition: types.DeliveryCondition{Condition: &types.DeliveryCondition_Hashlock{
			Hashlock: &types.HashlockCondition{PayloadHash: payloadHash, TimeoutBlocks: timeoutBlocks},
		}},
		ChamberId: 0,
	}
}

// newOptimisticEscrow assembles (does not store) an OPTIMISTIC escrow record.
func newOptimisticEscrow(id string, windowBlocks uint64, amount sdk.Coins) types.Escrow {
	return types.Escrow{
		Id:         id,
		PayerSigil: buyerSigil,
		PayeeSigil: sellerSigil,
		Amount:     amount,
		Condition: types.DeliveryCondition{Condition: &types.DeliveryCondition_Optimistic{
			Optimistic: &types.OptimisticCondition{DisputeWindowBlocks: windowBlocks},
		}},
		ChamberId: 0,
	}
}

func sha256Of(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

// bondOf is the expected OPTIMISTIC seller bond for a payment.
func bondOf(amount sdk.Coins) sdk.Coins { return types.SellerBondCoins(amount) }
