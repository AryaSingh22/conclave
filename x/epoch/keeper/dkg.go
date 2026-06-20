package keeper

import (
	"encoding/binary"
	"errors"
	"sort"
	"strconv"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/epoch/types"
)

// dkgRound identifies which transport window a bundle belongs to.
type dkgRound int

const (
	roundDeal dkgRound = iota
	roundResponse
	roundConfirm
)

// formingInWindow loads the FORMING epoch and checks that epochID matches it and
// that the given round's window is open at the current height. It is the shared
// gate for the three Collect* entry points (PHASE1_DESIGN §2(b) on-chain
// transport). The windows are sequential and non-overlapping:
//
//	deal     [formed_height, deal_deadline)
//	response [deal_deadline, response_deadline)
//	confirm  [response_deadline, confirm_deadline)
func (k Keeper) formingInWindow(ctx sdk.Context, epochID uint64, params types.Params, round dkgRound) (types.Epoch, error) {
	formingID := k.FormingEpochID(ctx)
	if formingID == 0 {
		return types.Epoch{}, types.ErrNoFormingEpoch
	}
	if epochID != formingID {
		return types.Epoch{}, errorsmod.Wrapf(types.ErrEpochMismatch, "got %d, forming %d", epochID, formingID)
	}
	e, ok := k.GetEpoch(ctx, formingID)
	if !ok {
		return types.Epoch{}, types.ErrEpochNotFound
	}
	height := uint64(ctx.BlockHeight())
	responseDeadline := e.DealDeadline + params.ResponseWindowBlocks
	var open bool
	switch round {
	case roundDeal:
		open = height < e.DealDeadline
	case roundResponse:
		open = height >= e.DealDeadline && height < responseDeadline
	case roundConfirm:
		open = height >= responseDeadline && height < e.ConfirmDeadline
	}
	if !open {
		return types.Epoch{}, errorsmod.Wrapf(types.ErrWindowClosed,
			"height %d not in round %d window of epoch %d", height, round, epochID)
	}
	return e, nil
}

// checkIndex confirms dkgIndex is a real committee position.
func checkIndex(e types.Epoch, dkgIndex uint32) error {
	if int(dkgIndex) >= len(e.Committee) {
		return errorsmod.Wrapf(types.ErrNotCommittee, "dkg_index %d out of range (n=%d)", dkgIndex, len(e.Committee))
	}
	return nil
}

// CollectDeal stores a Round-1 deal bundle (opaque bytes) for the FORMING epoch.
// The chain NEVER interprets the bundle — only validators do, off-chain (D-061).
func (k Keeper) CollectDeal(ctx sdk.Context, epochID uint64, dkgIndex uint32, bundle []byte) error {
	return k.collectBundle(ctx, epochID, dkgIndex, bundle, roundDeal, types.DealKey)
}

// CollectResponse stores a Round-2 response bundle (opaque bytes).
func (k Keeper) CollectResponse(ctx sdk.Context, epochID uint64, dkgIndex uint32, bundle []byte) error {
	return k.collectBundle(ctx, epochID, dkgIndex, bundle, roundResponse, types.ResponseKey)
}

func (k Keeper) collectBundle(ctx sdk.Context, epochID uint64, dkgIndex uint32, bundle []byte, round dkgRound, keyFn func(uint64, uint32) []byte) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if uint32(len(bundle)) > params.MaxBundleBytes {
		return errorsmod.Wrapf(types.ErrBundleTooLarge, "%d > %d", len(bundle), params.MaxBundleBytes)
	}
	e, err := k.formingInWindow(ctx, epochID, params, round)
	if err != nil {
		return err
	}
	if err := checkIndex(e, dkgIndex); err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)
	key := keyFn(epochID, dkgIndex)
	if has, _ := store.Has(key); has {
		return errorsmod.Wrapf(types.ErrDuplicateBundle, "epoch %d index %d", epochID, dkgIndex)
	}
	return store.Set(key, bundle)
}

// CollectConfirm stores a Round-3 key confirmation: the node's locally finalized
// epoch public key + verification polynomial, in a canonical encoding finalize
// counts agreement over.
func (k Keeper) CollectConfirm(ctx sdk.Context, epochID uint64, dkgIndex uint32, epochPub []byte, commits [][]byte) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	e, err := k.formingInWindow(ctx, epochID, params, roundConfirm)
	if err != nil {
		return err
	}
	if err := checkIndex(e, dkgIndex); err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)
	key := types.ConfirmKey(epochID, dkgIndex)
	if has, _ := store.Has(key); has {
		return errorsmod.Wrapf(types.ErrDuplicateBundle, "confirm epoch %d index %d", epochID, dkgIndex)
	}
	return store.Set(key, encodeConfirmValue(epochPub, commits))
}

// bundlesFor returns every stored bundle of one epoch under a prefix, in
// ascending dkg-index order (deterministic).
func (k Keeper) bundlesFor(ctx sdk.Context, prefix []byte, epochID uint64) [][]byte {
	store := k.storeService.OpenKVStore(ctx)
	iterPrefix := types.BundleIterPrefix(prefix, epochID)
	it, err := store.Iterator(iterPrefix, storePrefixEnd(iterPrefix))
	if err != nil {
		panic(err)
	}
	defer it.Close()
	var out [][]byte
	for ; it.Valid(); it.Next() {
		out = append(out, append([]byte{}, it.Value()...))
	}
	return out
}

// Deals returns the collected Round-1 bundles for an epoch (harness/transport).
func (k Keeper) Deals(ctx sdk.Context, epochID uint64) [][]byte {
	return k.bundlesFor(ctx, types.DealPrefix, epochID)
}

// Responses returns the collected Round-2 bundles for an epoch.
func (k Keeper) Responses(ctx sdk.Context, epochID uint64) [][]byte {
	return k.bundlesFor(ctx, types.ResponsePrefix, epochID)
}

// FinalizeForming runs at the close of the confirm window of the FORMING epoch:
// it publishes the (epoch_pub_key, commits) value that >= t committee members
// confirmed byte-identically, makes that epoch ACTIVE, and rotates; if no value
// reaches t the epoch is FAILED and there is NO rotation (graceful degradation —
// the chain never halts and the prior ACTIVE key stays the encryption target,
// the natural form of the §2(b) grace-window fallback). DETERMINISTIC: it counts
// agreement over committed bytes; no crypto runs here.
func (k Keeper) FinalizeForming(ctx sdk.Context, e types.Epoch) {
	k.setFormingEpochID(ctx, 0)

	value, count, ok := k.tallyConfirmations(ctx, e.Id, int(e.Threshold))
	if !ok {
		e.Status = types.EPOCH_STATUS_FAILED
		k.SetEpoch(ctx, e)
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeEpochFailed,
			sdk.NewAttribute(types.AttributeKeyEpochID, strconv.FormatUint(e.Id, 10)),
			sdk.NewAttribute(types.AttributeKeyFailureReason, "insufficient confirmations"),
		))
		k.gcTranscript(ctx, e.Id)
		return
	}

	pub, commits, err := decodeConfirmValue(value)
	if err != nil {
		// a stored confirmation that cannot decode is state corruption, not user
		// input; fail the epoch rather than publish garbage.
		e.Status = types.EPOCH_STATUS_FAILED
		k.SetEpoch(ctx, e)
		k.gcTranscript(ctx, e.Id)
		return
	}
	e.Status = types.EPOCH_STATUS_ACTIVE
	e.EpochPubKey = pub
	e.Commits = commits
	k.SetEpoch(ctx, e)
	k.rotateTo(ctx, e.Id)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeEpochActive,
		sdk.NewAttribute(types.AttributeKeyEpochID, strconv.FormatUint(e.Id, 10)),
		sdk.NewAttribute(types.AttributeKeyConfirmations, strconv.Itoa(count)),
	))
	k.gcTranscript(ctx, e.Id)
}

// tallyConfirmations groups the epoch's confirmations by their canonical value
// bytes and returns the value with >= t confirmations. Ties (multiple values at
// >= t — impossible with an honest majority, but pinned for determinism) break
// to the lexicographically smallest value. Iteration is index-ascending and the
// grouping is over a sorted key list, so the result is deterministic (§11.6).
func (k Keeper) tallyConfirmations(ctx sdk.Context, epochID uint64, t int) (value []byte, count int, ok bool) {
	confirms := k.bundlesFor(ctx, types.ConfirmPrefix, epochID)
	counts := make(map[string]int, len(confirms))
	for _, c := range confirms {
		counts[string(c)]++
	}
	keys := make([]string, 0, len(counts))
	for v := range counts {
		keys = append(keys, v)
	}
	sort.Strings(keys)
	for _, v := range keys {
		if counts[v] >= t {
			return []byte(v), counts[v], true
		}
	}
	return nil, 0, false
}

// gcTranscript deletes the per-index deal/response/confirm bundles of a
// finalized epoch — they are transient transport, not lasting epoch state.
func (k Keeper) gcTranscript(ctx sdk.Context, epochID uint64) {
	store := k.storeService.OpenKVStore(ctx)
	for _, prefix := range [][]byte{types.DealPrefix, types.ResponsePrefix, types.ConfirmPrefix} {
		iterPrefix := types.BundleIterPrefix(prefix, epochID)
		it, err := store.Iterator(iterPrefix, storePrefixEnd(iterPrefix))
		if err != nil {
			panic(err)
		}
		var keys [][]byte
		for ; it.Valid(); it.Next() {
			keys = append(keys, append([]byte{}, it.Key()...))
		}
		it.Close()
		for _, key := range keys {
			if err := store.Delete(key); err != nil {
				panic(err)
			}
		}
	}
}

// --- canonical confirm-value encoding (length-prefixed; deterministic) ---

func encodeConfirmValue(pub []byte, commits [][]byte) []byte {
	buf := appendBytes(nil, pub)
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(commits)))
	buf = append(buf, n[:]...)
	for _, c := range commits {
		buf = appendBytes(buf, c)
	}
	return buf
}

func decodeConfirmValue(b []byte) (pub []byte, commits [][]byte, err error) {
	pub, b, err = readBytes(b)
	if err != nil {
		return nil, nil, err
	}
	if len(b) < 4 {
		return nil, nil, errors.New("epoch: truncated commits count")
	}
	n := binary.BigEndian.Uint32(b[:4])
	b = b[4:]
	commits = make([][]byte, 0, n)
	for i := uint32(0); i < n; i++ {
		var c []byte
		c, b, err = readBytes(b)
		if err != nil {
			return nil, nil, err
		}
		commits = append(commits, c)
	}
	if len(b) != 0 {
		return nil, nil, errors.New("epoch: trailing bytes in confirm value")
	}
	return pub, commits, nil
}

func appendBytes(buf, chunk []byte) []byte {
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(chunk)))
	buf = append(buf, l[:]...)
	return append(buf, chunk...)
}

func readBytes(b []byte) (chunk, rest []byte, err error) {
	if len(b) < 4 {
		return nil, nil, errors.New("epoch: truncated length prefix")
	}
	n := binary.BigEndian.Uint32(b[:4])
	if uint64(len(b)-4) < uint64(n) {
		return nil, nil, errors.New("epoch: truncated chunk")
	}
	return b[4 : 4+n], b[4+n:], nil
}
