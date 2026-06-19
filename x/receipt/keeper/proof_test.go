package keeper

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	commitmenttypes "github.com/cosmos/ibc-go/v8/modules/core/23-commitment/types"

	"github.com/AryaSingh22/conclave/crypto/reveal"
	mandatekeeper "github.com/AryaSingh22/conclave/x/mandate/keeper"
	mandatetypes "github.com/AryaSingh22/conclave/x/mandate/types"
	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	sessionkeeper "github.com/AryaSingh22/conclave/x/session/keeper"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	sigilkeeper "github.com/AryaSingh22/conclave/x/sigil/keeper"
	sigiltypes "github.com/AryaSingh22/conclave/x/sigil/types"
	veilkeeper "github.com/AryaSingh22/conclave/x/veil/keeper"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

const (
	bondDenom = "uclv"
	usdcDenom = "uusdc"
	svcDenom  = "svc/cmpt"
)

// --- minimal mocks (mirroring x/session/keeper's test mocks; cross-package) ---

type mockBank struct{ balances map[string]sdk.Coins }

func newMockBank() *mockBank { return &mockBank{balances: make(map[string]sdk.Coins)} }

func (b *mockBank) fund(addr string, coins sdk.Coins) {
	b.balances[addr] = b.balances[addr].Add(coins...)
}

func (b *mockBank) send(from, to string, amt sdk.Coins) error {
	have := b.balances[from]
	if !amt.IsAllLTE(have) {
		return errorsmod.Wrapf(sessiontypes.ErrInvalidBond, "mock bank: %s holds %s, needs %s", from, have, amt)
	}
	b.balances[from] = have.Sub(amt...)
	b.balances[to] = b.balances[to].Add(amt...)
	return nil
}

func (b *mockBank) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error {
	return b.send(sender.String(), authtypes.NewModuleAddress(module).String(), amt)
}

func (b *mockBank) SendCoinsFromModuleToAccount(_ context.Context, module string, recipient sdk.AccAddress, amt sdk.Coins) error {
	return b.send(authtypes.NewModuleAddress(module).String(), recipient.String(), amt)
}

func (b *mockBank) SendCoinsFromModuleToModule(_ context.Context, senderModule, recipientModule string, amt sdk.Coins) error {
	return b.send(authtypes.NewModuleAddress(senderModule).String(), authtypes.NewModuleAddress(recipientModule).String(), amt)
}

func (b *mockBank) MintCoins(_ context.Context, module string, amt sdk.Coins) error {
	addr := authtypes.NewModuleAddress(module).String()
	b.balances[addr] = b.balances[addr].Add(amt...)
	return nil
}

func (b *mockBank) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.balances[addr.String()].AmountOf(denom))
}

type mockDistr struct{ bank *mockBank }

func (d mockDistr) FundCommunityPool(_ context.Context, amount sdk.Coins, sender sdk.AccAddress) error {
	return d.bank.send(sender.String(), authtypes.NewModuleAddress(distrtypes.ModuleName).String(), amount)
}

type mockAccount struct{}

func (mockAccount) GetModuleAddress(name string) sdk.AccAddress {
	return authtypes.NewModuleAddress(name)
}

func (mockAccount) GetModuleAccount(_ context.Context, name string) sdk.ModuleAccountI {
	return authtypes.NewEmptyModuleAccount(name)
}

// --- fixture wiring the full settlement stack + the receipt query/proof keeper ---

type fixture struct {
	ctx        sdk.Context
	cms        storetypes.CommitMultiStore
	sessionKey *storetypes.KVStoreKey
	qs         receipttypes.QueryServer
	session    *sessionkeeper.Keeper
	veil       veilkeeper.Keeper
	sigil      sigilkeeper.Keeper
	bank       *mockBank
}

func setupFixture(t *testing.T) *fixture {
	t.Helper()
	sessionKey := storetypes.NewKVStoreKey(sessiontypes.StoreKey)
	veilKey := storetypes.NewKVStoreKey(veiltypes.StoreKey)
	sigilKey := storetypes.NewKVStoreKey(sigiltypes.StoreKey)
	mandateKey := storetypes.NewKVStoreKey(mandatetypes.StoreKey)
	// Build the commit multistore exactly as baseapp does — each IAVL store on a
	// prefixed view of one db (nil per-store db) so they don't collide on Commit.
	// (testutil.DefaultContextWithKeys shares one db across stores, which is fine
	// for read/write but collides at Commit; this test commits, so it can't use
	// it.)
	db := dbm.NewMemDB()
	cms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	for _, k := range []*storetypes.KVStoreKey{sessionKey, veilKey, sigilKey, mandateKey} {
		cms.MountStoreWithDB(k, storetypes.StoreTypeIAVL, nil)
	}
	require.NoError(t, cms.LoadLatestVersion())
	ctx := sdk.NewContext(cms, cmtproto.Header{}, false, log.NewNopLogger())
	encCfg := moduletestutil.MakeTestEncodingConfig()
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	bank := newMockBank()
	sk := sigilkeeper.NewKeeper(encCfg.Codec, runtime.NewKVStoreService(sigilKey), authority)
	mk := mandatekeeper.NewKeeper(encCfg.Codec, runtime.NewKVStoreService(mandateKey), sk, bank, mockAccount{}, authority)
	sessionK := sessionkeeper.NewKeeper(encCfg.Codec, runtime.NewKVStoreService(sessionKey), bank, mockDistr{bank: bank}, mockAccount{}, sk, authority)
	veilK := veilkeeper.NewKeeper(
		encCfg.Codec, runtime.NewKVStoreService(veilKey),
		reveal.StubScheme{}, bank, mockDistr{bank: bank}, sk, mk,
		sessionK, mandatetypes.FaceValuePriceSource{}, authority,
	)
	sessionK.SetVeilKeeper(veilK)

	ctx = ctx.WithBlockHeight(1)
	require.NoError(t, sk.SetParams(ctx, sigiltypes.DefaultParams()))
	require.NoError(t, mk.SetParams(ctx, mandatetypes.DefaultParams()))
	require.NoError(t, veilK.SetParams(ctx, veiltypes.DefaultParams()))
	require.NoError(t, sessionK.SetParams(ctx, sessiontypes.DefaultParams()))

	// The committed multistore is the ICS-23 ProofQuerier (D-053): rootmulti's
	// Query appends the multistore commit op chaining to the app root. This is
	// the same object the real app passes (app.CommitMultiStore()).
	rk := NewKeeper(encCfg.Codec, sessionK, cms.(receipttypes.ProofQuerier))

	return &fixture{
		ctx:        ctx,
		cms:        cms,
		sessionKey: sessionKey,
		qs:         NewQueryServerImpl(rk),
		session:    sessionK,
		veil:       veilK,
		sigil:      sk,
		bank:       bank,
	}
}

func randAddr() sdk.AccAddress { return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()) }

func (f *fixture) registerSigil(t *testing.T, controller sdk.AccAddress) string {
	t.Helper()
	h := sha256.Sum256([]byte("did-doc" + controller.String()))
	id, err := f.sigil.RegisterSigil(f.ctx, controller, h[:], "ipfs://meta", nil, 0, 0)
	require.NoError(t, err)
	return id
}

func coinLeg(c sdk.Coin) veiltypes.IntentLeg {
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Coin{Coin: &c}}
}

func svcLeg(schema string, units int64) veiltypes.IntentLeg {
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Svc{Svc: &veiltypes.ServiceVoucher{Schema: schema, Units: math.NewInt(units)}}}
}

func dataLeg(contentHash []byte) veiltypes.IntentLeg {
	return veiltypes.IntentLeg{Asset: &veiltypes.IntentLeg_Data{Data: &veiltypes.DataGrant{ContentHash: contentHash}}}
}

func minBond() sdk.Coin { return sdk.NewInt64Coin(bondDenom, 1_000_000) }

func (f *fixture) submitOpen(t *testing.T, owner sdk.AccAddress, sigilID string, give, want []veiltypes.IntentLeg, ttl uint64) string {
	t.Helper()
	f.bank.fund(owner.String(), sdk.NewCoins(minBond()))
	p := veiltypes.IntentPayload{Give: give, Want: want}
	pbz, err := p.Marshal()
	require.NoError(t, err)
	// declare spend_commitment = the coin give value so the Reveal opens the
	// intent rather than slashing it as an under-declaration (D-065); svc/data
	// give legs value 0, so a non-coin give honestly declares zero.
	commitment := sdk.NewCoins()
	for i := range give {
		if c := give[i].GetCoin(); c != nil && c.IsValid() && !c.IsZero() {
			commitment = commitment.Max(sdk.NewCoins(*c))
		}
	}
	env := veiltypes.IntentEnvelope{TtlHeight: ttl, SpendCommitment: commitment}
	ebz, err := env.Marshal()
	require.NoError(t, err)
	id, err := f.veil.SubmitIntent(f.ctx, &veiltypes.MsgSubmitIntent{
		Sender: owner.String(), SenderSigil: sigilID,
		Ciphertext: pbz, Envelope: ebz, Bond: minBond(),
	})
	require.NoError(t, err)
	return id
}

func contentHash(tag string) []byte { h := sha256.Sum256([]byte(tag)); return h[:] }

// cycle is the canonical 3-party cycle: alice 100 uusdc → data D; bob D → 10
// CMPT; carol 10 CMPT → 100 uusdc. After Conclude each party holds one receipt.
type cycle struct {
	aliceSigil, bobSigil, carolSigil string
	alice                            sdk.AccAddress
	aliceID, bobID, carolID          string
	dataHash                         []byte
	sessionID                        uint64
}

func (f *fixture) setupCycleAndConclude(t *testing.T) cycle {
	t.Helper()
	c := cycle{dataHash: contentHash("dataset-D")}
	c.alice = randAddr()
	bob, carol := randAddr(), randAddr()
	c.aliceSigil = f.registerSigil(t, c.alice)
	c.bobSigil = f.registerSigil(t, bob)
	c.carolSigil = f.registerSigil(t, carol)

	f.bank.fund(c.alice.String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000)))
	usdc := sdk.NewInt64Coin(usdcDenom, 100_000_000)
	c.aliceID = f.submitOpen(t, c.alice, c.aliceSigil, []veiltypes.IntentLeg{coinLeg(usdc)}, []veiltypes.IntentLeg{dataLeg(c.dataHash)}, 100)
	c.bobID = f.submitOpen(t, bob, c.bobSigil, []veiltypes.IntentLeg{dataLeg(c.dataHash)}, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, 100)
	c.carolID = f.submitOpen(t, carol, c.carolSigil, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, []veiltypes.IntentLeg{coinLeg(usdc)}, 100)

	f.ctx = f.ctx.WithBlockHeight(3)
	require.NoError(t, f.veil.SealSubmitted(f.ctx, 3))
	f.ctx = f.ctx.WithBlockHeight(4)
	require.NoError(t, f.veil.RevealSealed(f.ctx))
	c.sessionID = 3

	matches := []sessiontypes.Match{{
		IntentIds: []string{c.aliceID, c.bobID, c.carolID},
		Legs: []sessiontypes.SettleLeg{
			{FromSigil: c.aliceSigil, ToSigil: c.carolSigil, Asset: &sessiontypes.SettleLeg_Coin{Coin: &usdc}},
			{FromSigil: c.bobSigil, ToSigil: c.aliceSigil, Asset: &sessiontypes.SettleLeg_Data{Data: &veiltypes.DataGrant{ContentHash: c.dataHash}}},
			{FromSigil: c.carolSigil, ToSigil: c.bobSigil, Asset: &sessiontypes.SettleLeg_Svc{Svc: &veiltypes.ServiceVoucher{Schema: "cmpt", Units: math.NewInt(10)}}},
		},
		ClearingPrices: []sessiontypes.PricePoint{
			{Base: usdcDenom, Quote: usdcDenom, Price: math.LegacyOneDec()},
			{Base: "data/" + sessiontypes.IDHex(c.dataHash), Quote: usdcDenom, Price: math.LegacyNewDec(100_000_000)},
			{Base: svcDenom, Quote: usdcDenom, Price: math.LegacyNewDec(10_000_000)},
		},
	}}
	require.NoError(t, f.session.Conclude(f.ctx, c.sessionID, matches))
	return c
}

// commit flushes the working tree into a new IAVL version and returns the
// committed app root that ICS-23 proofs verify against.
func (f *fixture) commit() []byte {
	f.cms.Commit()
	return f.cms.LastCommitID().Hash
}

// verifyReceiptProof runs the full ICS-23 verification a remote IBC counterparty
// would: ConvertProofs → VerifyMembership of the receipt value against the app
// root at the claimed proof path (D-053).
func verifyReceiptProof(root []byte, proofBz []byte, key []byte, value []byte) error {
	var proofOps cmtprotocrypto.ProofOps
	if err := proofOps.Unmarshal(proofBz); err != nil {
		return err
	}
	merkleProof, err := commitmenttypes.ConvertProofs(&proofOps)
	if err != nil {
		return err
	}
	return merkleProof.VerifyMembership(
		commitmenttypes.GetSDKSpecs(),
		commitmenttypes.NewMerkleRoot(root),
		commitmenttypes.NewMerklePath(sessiontypes.StoreKey, string(key)),
		value,
	)
}

// TestReceiptProofVerifies is the §10 row-0.9 core: the ICS-23 proof returned by
// ReceiptProof actually VERIFIES against the committed store root — and fails on
// both a tampered value (sensitivity) and a stale root (height-binding).
func TestReceiptProofVerifies(t *testing.T) {
	f := setupFixture(t)
	c := f.setupCycleAndConclude(t)
	rootH := f.commit()

	// fetch alice's receipt id from the query surface, then its proof.
	list, err := f.qs.Receipts(f.ctx, &receipttypes.QueryReceiptsRequest{SigilId: c.aliceSigil})
	require.NoError(t, err)
	require.Len(t, list.Receipts, 1)
	receiptID := list.Receipts[0].Id

	resp, err := f.qs.ReceiptProof(f.ctx, &receipttypes.QueryReceiptProofRequest{Id: receiptID})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Proof)
	require.Equal(t, receiptID, resp.Receipt.Id)

	key, err := f.session.ReceiptByIDStoreKey(receiptID)
	require.NoError(t, err)
	// The committed value bytes equal the codec-marshaled receipt (gogoproto
	// Marshal is deterministic and identical to what x/session stored).
	value, err := resp.Receipt.Marshal()
	require.NoError(t, err)

	// (1) positive: the proof verifies against the committed root at H.
	require.NoError(t, verifyReceiptProof(rootH, resp.Proof, key, value),
		"ICS-23 proof must verify against the store root")

	// (2) negative — byte-flip: a tampered value must NOT verify (sensitivity).
	tampered := append([]byte{}, value...)
	tampered[0] ^= 0xFF
	require.Error(t, verifyReceiptProof(rootH, resp.Proof, key, tampered),
		"proof must reject a tampered value")

	// (3) negative — staleness: advance the store root, and the SAME proof must
	// fail against the NEW root while still verifying against the root at H.
	// This is what makes ProofHeight load-bearing (D-053).
	require.NoError(t, f.session.SetParams(f.ctx, sessiontypes.DefaultParams())) // a real write
	f.ctx.KVStore(f.sessionKey).Set([]byte{0xFE, 0x01}, []byte("advance-root"))
	rootH1 := f.commit()
	require.NotEqual(t, rootH, rootH1, "root must advance for a meaningful staleness test")

	require.Error(t, verifyReceiptProof(rootH1, resp.Proof, key, value),
		"proof bound to height H must NOT verify against a later root")
	require.NoError(t, verifyReceiptProof(rootH, resp.Proof, key, value),
		"proof still verifies against the correct root at its ProofHeight")
}

// TestReceiptsPagination exercises the §6.6 per-agent index (0x07) with working
// pagination: alice settles three intents → three receipts; Limit:2 returns 2 +
// a next_key, and following the key returns the remainder.
func TestReceiptsPagination(t *testing.T) {
	f := setupFixture(t)

	// alice with three distinct give/want intents that all settle pairwise.
	alice := randAddr()
	bob := randAddr()
	aliceSigil := f.registerSigil(t, alice)
	bobSigil := f.registerSigil(t, bob)
	f.bank.fund(alice.String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 300_000_000)))

	type pair struct {
		aID, bID string
		tag      string
	}
	var pairs []pair
	for i := 0; i < 3; i++ {
		tag := "ds-" + string(rune('A'+i))
		dh := contentHash(tag)
		usdc := sdk.NewInt64Coin(usdcDenom, 100_000_000)
		aID := f.submitOpen(t, alice, aliceSigil, []veiltypes.IntentLeg{coinLeg(usdc)}, []veiltypes.IntentLeg{dataLeg(dh)}, 100)
		bID := f.submitOpen(t, bob, bobSigil, []veiltypes.IntentLeg{dataLeg(dh)}, []veiltypes.IntentLeg{coinLeg(usdc)}, 100)
		pairs = append(pairs, pair{aID, bID, tag})
	}
	f.ctx = f.ctx.WithBlockHeight(3)
	require.NoError(t, f.veil.SealSubmitted(f.ctx, 3))
	f.ctx = f.ctx.WithBlockHeight(4)
	require.NoError(t, f.veil.RevealSealed(f.ctx))

	usdc := sdk.NewInt64Coin(usdcDenom, 100_000_000)
	var matches []sessiontypes.Match
	for _, p := range pairs {
		dh := contentHash(p.tag)
		matches = append(matches, sessiontypes.Match{
			IntentIds: []string{p.aID, p.bID},
			Legs: []sessiontypes.SettleLeg{
				{FromSigil: aliceSigil, ToSigil: bobSigil, Asset: &sessiontypes.SettleLeg_Coin{Coin: &usdc}},
				{FromSigil: bobSigil, ToSigil: aliceSigil, Asset: &sessiontypes.SettleLeg_Data{Data: &veiltypes.DataGrant{ContentHash: dh}}},
			},
			ClearingPrices: []sessiontypes.PricePoint{
				{Base: usdcDenom, Quote: usdcDenom, Price: math.LegacyOneDec()},
				{Base: "data/" + sessiontypes.IDHex(dh), Quote: usdcDenom, Price: math.LegacyNewDec(100_000_000)},
			},
		})
	}
	require.NoError(t, f.session.Conclude(f.ctx, 3, matches))

	// page 1: limit 2 → 2 receipts + non-nil next_key.
	page1, err := f.qs.Receipts(f.ctx, &receipttypes.QueryReceiptsRequest{
		SigilId:    aliceSigil,
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, page1.Receipts, 2)
	require.NotNil(t, page1.Pagination)
	require.NotEmpty(t, page1.Pagination.NextKey)

	// page 2: follow next_key → the remaining receipt.
	page2, err := f.qs.Receipts(f.ctx, &receipttypes.QueryReceiptsRequest{
		SigilId:    aliceSigil,
		Pagination: &query.PageRequest{Key: page1.Pagination.NextKey},
	})
	require.NoError(t, err)
	require.Len(t, page2.Receipts, 1)

	// no overlap, three distinct receipts total.
	seen := map[string]bool{}
	for _, r := range append(append([]receipttypes.Receipt{}, page1.Receipts...), page2.Receipts...) {
		require.False(t, seen[r.Id], "receipt %s returned twice across pages", r.Id)
		seen[r.Id] = true
		require.Equal(t, aliceSigil, r.SigilId)
	}
	require.Len(t, seen, 3)
}

// TestReputationMovedAndSameIntentProofVerifies closes the §6.6 loop: the same
// CONCLUDED intent whose sigil reputation moved (§6.1 hook, fired by 0.8 inside
// the Conclude cache) has a ReceiptProof that cryptographically verifies —
// "verify the proof, not the indexer."
func TestReputationMovedAndSameIntentProofVerifies(t *testing.T) {
	f := setupFixture(t)

	// reputation BEFORE: register alice, read her starting score.
	c := cycle{dataHash: contentHash("dataset-D")}
	c.alice = randAddr()
	bob, carol := randAddr(), randAddr()
	c.aliceSigil = f.registerSigil(t, c.alice)
	c.bobSigil = f.registerSigil(t, bob)
	c.carolSigil = f.registerSigil(t, carol)
	repBefore, err := f.sigil.GetReputation(f.ctx, 0, c.aliceSigil)
	require.NoError(t, err)

	f.bank.fund(c.alice.String(), sdk.NewCoins(sdk.NewInt64Coin(usdcDenom, 100_000_000)))
	usdc := sdk.NewInt64Coin(usdcDenom, 100_000_000)
	c.aliceID = f.submitOpen(t, c.alice, c.aliceSigil, []veiltypes.IntentLeg{coinLeg(usdc)}, []veiltypes.IntentLeg{dataLeg(c.dataHash)}, 100)
	c.bobID = f.submitOpen(t, bob, c.bobSigil, []veiltypes.IntentLeg{dataLeg(c.dataHash)}, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, 100)
	c.carolID = f.submitOpen(t, carol, c.carolSigil, []veiltypes.IntentLeg{svcLeg("cmpt", 10)}, []veiltypes.IntentLeg{coinLeg(usdc)}, 100)
	f.ctx = f.ctx.WithBlockHeight(3)
	require.NoError(t, f.veil.SealSubmitted(f.ctx, 3))
	f.ctx = f.ctx.WithBlockHeight(4)
	require.NoError(t, f.veil.RevealSealed(f.ctx))
	matches := []sessiontypes.Match{{
		IntentIds: []string{c.aliceID, c.bobID, c.carolID},
		Legs: []sessiontypes.SettleLeg{
			{FromSigil: c.aliceSigil, ToSigil: c.carolSigil, Asset: &sessiontypes.SettleLeg_Coin{Coin: &usdc}},
			{FromSigil: c.bobSigil, ToSigil: c.aliceSigil, Asset: &sessiontypes.SettleLeg_Data{Data: &veiltypes.DataGrant{ContentHash: c.dataHash}}},
			{FromSigil: c.carolSigil, ToSigil: c.bobSigil, Asset: &sessiontypes.SettleLeg_Svc{Svc: &veiltypes.ServiceVoucher{Schema: "cmpt", Units: math.NewInt(10)}}},
		},
		ClearingPrices: []sessiontypes.PricePoint{
			{Base: usdcDenom, Quote: usdcDenom, Price: math.LegacyOneDec()},
			{Base: "data/" + sessiontypes.IDHex(c.dataHash), Quote: usdcDenom, Price: math.LegacyNewDec(100_000_000)},
			{Base: svcDenom, Quote: usdcDenom, Price: math.LegacyNewDec(10_000_000)},
		},
	}}
	require.NoError(t, f.session.Conclude(f.ctx, 3, matches))

	// reputation MOVED on CONCLUDED (alice settled 100 USDC of numeraire).
	repAfter, err := f.sigil.GetReputation(f.ctx, 0, c.aliceSigil)
	require.NoError(t, err)
	require.True(t, repAfter.Score.GT(repBefore.Score),
		"alice's reputation must increase on CONCLUDED (got %s ≤ %s)", repAfter.Score, repBefore.Score)

	// the SAME settled intent's receipt proof verifies against the store root.
	root := f.commit()
	list, err := f.qs.Receipts(f.ctx, &receipttypes.QueryReceiptsRequest{SigilId: c.aliceSigil})
	require.NoError(t, err)
	require.Len(t, list.Receipts, 1)
	require.Equal(t, c.aliceID, list.Receipts[0].IntentId)

	resp, err := f.qs.ReceiptProof(f.ctx, &receipttypes.QueryReceiptProofRequest{Id: list.Receipts[0].Id})
	require.NoError(t, err)
	key, err := f.session.ReceiptByIDStoreKey(list.Receipts[0].Id)
	require.NoError(t, err)
	value, err := resp.Receipt.Marshal()
	require.NoError(t, err)
	require.NoError(t, verifyReceiptProof(root, resp.Proof, key, value),
		"the proof for the same intent whose reputation moved must verify")
}

// TestReceiptNotFound: an unknown id is NotFound for both Receipt and ReceiptProof.
func TestReceiptNotFound(t *testing.T) {
	f := setupFixture(t)
	missing := sessiontypes.IDHex(contentHash("nope"))
	_, err := f.qs.Receipt(f.ctx, &receipttypes.QueryReceiptRequest{Id: missing})
	require.Error(t, err)
	_, err = f.qs.ReceiptProof(f.ctx, &receipttypes.QueryReceiptProofRequest{Id: missing})
	require.Error(t, err)
}
