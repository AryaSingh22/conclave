package keeper

import (
	"bytes"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// SetMandate stores a mandate record (SPEC §6.2 key 0x01|chamber|grantee|id).
func (k Keeper) SetMandate(ctx sdk.Context, m types.Mandate) error {
	idBz, err := types.MandateIDBytes(m.Id)
	if err != nil {
		return err
	}
	grantee, err := sdk.AccAddressFromBech32(m.GranteeAddr)
	if err != nil {
		return err
	}
	bz, err := k.cdc.Marshal(&m)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(types.MandateKey(m.ChamberId, grantee, idBz), bz)
}

// GetMandate returns a mandate by chamber, grantee, and id.
func (k Keeper) GetMandate(ctx sdk.Context, chamberID uint32, grantee sdk.AccAddress, id string) (types.Mandate, error) {
	idBz, err := types.MandateIDBytes(id)
	if err != nil {
		return types.Mandate{}, err
	}
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.MandateKey(chamberID, grantee, idBz))
	if err != nil {
		return types.Mandate{}, err
	}
	if bz == nil {
		return types.Mandate{}, errorsmod.Wrapf(types.ErrMandateNotFound, "id %s", id)
	}
	var m types.Mandate
	if err := k.cdc.Unmarshal(bz, &m); err != nil {
		return types.Mandate{}, err
	}
	return m, nil
}

// deleteMandate removes a mandate record and its per-sigil index entry.
// The grantee index (0x02) is the caller's responsibility: it is only ever
// overwritten by a replacing issuance, never deleted (D-017).
func (k Keeper) deleteMandate(ctx sdk.Context, m types.Mandate) error {
	idBz, err := types.MandateIDBytes(m.Id)
	if err != nil {
		return err
	}
	grantee, err := sdk.AccAddressFromBech32(m.GranteeAddr)
	if err != nil {
		return err
	}
	sigilIDBz, err := decodeSigilID(m.SigilId)
	if err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)
	if err := store.Delete(types.MandateKey(m.ChamberId, grantee, idBz)); err != nil {
		return err
	}
	return store.Delete(types.SigilIndexKey(m.ChamberId, sigilIDBz, idBz))
}

// setIndexes writes the per-grantee active-mandate index (SPEC §6.2 key
// 0x02) and the per-sigil secondary index (0x03) for a mandate.
func (k Keeper) setIndexes(ctx sdk.Context, m types.Mandate, grantee sdk.AccAddress) error {
	idBz, err := types.MandateIDBytes(m.Id)
	if err != nil {
		return err
	}
	sigilIDBz, err := decodeSigilID(m.SigilId)
	if err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)
	if err := store.Set(types.GranteeIndexKey(m.ChamberId, grantee), idBz); err != nil {
		return err
	}
	return store.Set(types.SigilIndexKey(m.ChamberId, sigilIDBz, idBz), grantee.Bytes())
}

// ActiveMandateByGrantee implements the SPEC §6.2 O(1) ante lookup: resolve
// the grantee's single indexed mandate, if any. found=false means the key
// has no mandate history and is a root/controller key (step 1 passthrough).
// The returned mandate may be revoked or expired — index entries are never
// deleted on revocation/expiry (D-017), so a once-mandated key can never
// silently regain root passthrough.
func (k Keeper) ActiveMandateByGrantee(ctx sdk.Context, chamberID uint32, grantee sdk.AccAddress) (types.Mandate, bool, error) {
	store := k.storeService.OpenKVStore(ctx)
	idBz, err := store.Get(types.GranteeIndexKey(chamberID, grantee))
	if err != nil {
		return types.Mandate{}, false, err
	}
	if idBz == nil {
		return types.Mandate{}, false, nil
	}
	bz, err := store.Get(types.MandateKey(chamberID, grantee, idBz))
	if err != nil {
		return types.Mandate{}, false, err
	}
	if bz == nil {
		return types.Mandate{}, false, errorsmod.Wrapf(types.ErrMandateNotFound,
			"grantee index points to missing mandate %x", idBz)
	}
	var m types.Mandate
	if err := k.cdc.Unmarshal(bz, &m); err != nil {
		return types.Mandate{}, false, err
	}
	return m, true, nil
}

// MandateByID resolves a mandate by id alone via a deterministic prefix walk
// (msg-server and query paths; the ante hot path uses the 0x02 index).
func (k Keeper) MandateByID(ctx sdk.Context, id string) (types.Mandate, error) {
	idBz, err := types.MandateIDBytes(id)
	if err != nil {
		return types.Mandate{}, err
	}
	var found *types.Mandate
	if err := k.IterateMandates(ctx, func(m types.Mandate) bool {
		mIDBz, err := types.MandateIDBytes(m.Id)
		if err == nil && bytes.Equal(mIDBz, idBz) {
			found = &m
			return true
		}
		return false
	}); err != nil {
		return types.Mandate{}, err
	}
	if found == nil {
		return types.Mandate{}, errorsmod.Wrapf(types.ErrMandateNotFound, "id %s", id)
	}
	return *found, nil
}

// IterateMandates walks all mandate records (all chambers), stopping when cb
// returns true.
func (k Keeper) IterateMandates(ctx sdk.Context, cb func(types.Mandate) bool) error {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.MandateKeyPrefix, append(types.MandateKeyPrefix, 0xFF))
	if err != nil {
		return err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var m types.Mandate
		if err := k.cdc.Unmarshal(it.Value(), &m); err != nil {
			return err
		}
		if cb(m) {
			break
		}
	}
	return nil
}

// IterateGranteeIndex walks all 0x02 active-mandate index entries.
func (k Keeper) IterateGranteeIndex(ctx sdk.Context, cb func(chamberID uint32, grantee sdk.AccAddress, mandateIDBz []byte) bool) error {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.GranteeIndexKeyPrefix, append(types.GranteeIndexKeyPrefix, 0xFF))
	if err != nil {
		return err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		key := it.Key()
		// key = 0x02 | chamber(4) | grantee_addr
		chamberID := uint32(key[1])<<24 | uint32(key[2])<<16 | uint32(key[3])<<8 | uint32(key[4])
		grantee := sdk.AccAddress(key[5:])
		if cb(chamberID, grantee, it.Value()) {
			break
		}
	}
	return nil
}

// MandatesBySigil returns all mandates of a sigil via the 0x03 index.
func (k Keeper) MandatesBySigil(ctx sdk.Context, chamberID uint32, sigilID string) ([]types.Mandate, error) {
	sigilIDBz, err := decodeSigilID(sigilID)
	if err != nil {
		return nil, err
	}
	store := k.storeService.OpenKVStore(ctx)
	prefix := types.SigilIndexIterPrefix(chamberID, sigilIDBz)
	it, err := store.Iterator(prefix, append(prefix, 0xFF))
	if err != nil {
		return nil, err
	}
	defer it.Close()

	var mandates []types.Mandate
	for ; it.Valid(); it.Next() {
		key := it.Key()
		// key = 0x03 | chamber(4) | sigil_id(32) | mandate_id(32); value = grantee addr
		mandateIDBz := key[len(key)-types.IDLength:]
		grantee := sdk.AccAddress(it.Value())
		bz, err := store.Get(types.MandateKey(chamberID, grantee, mandateIDBz))
		if err != nil {
			return nil, err
		}
		if bz == nil {
			return nil, errorsmod.Wrapf(types.ErrMandateNotFound, "sigil index points to missing mandate %x", mandateIDBz)
		}
		var m types.Mandate
		if err := k.cdc.Unmarshal(bz, &m); err != nil {
			return nil, err
		}
		mandates = append(mandates, m)
	}
	return mandates, nil
}

// IssueMandate implements SPEC §6.2 MsgIssueMandate: the signer must be the
// sigil controller; issuing to a grantee that already holds a mandate
// replaces it (open decision D2: one active mandate per key, REPLACE).
func (k Keeper) IssueMandate(ctx sdk.Context, controller sdk.AccAddress, terms types.MandateTerms) (string, error) {
	if err := terms.Validate(); err != nil {
		return "", err
	}
	if terms.ExpiryHeight <= uint64(ctx.BlockHeight()) {
		return "", errorsmod.Wrapf(types.ErrInvalidTerms,
			"expiry_height %d is not beyond current height %d", terms.ExpiryHeight, ctx.BlockHeight())
	}

	// signer must be the sigil controller (SPEC §6.2 "Msgs")
	sigilController, err := k.sigilKeeper.ControllerAddress(ctx, terms.ChamberId, terms.SigilId)
	if err != nil {
		return "", err
	}
	if !sigilController.Equals(controller) {
		return "", errorsmod.Wrapf(types.ErrUnauthorized,
			"signer %s is not the controller of sigil %s", controller, terms.SigilId)
	}

	grantee := sdk.MustAccAddressFromBech32(terms.GranteeAddr)

	// a grantee that controls a sigil can never be mandate-bound: its txs
	// must stay root-key governed, and binding it would either break the
	// single-valued reverse index (D-010) or brick the controller (the
	// privilege-escalation guard forbids allowing MsgRevokeMandate, so a
	// self-mandated controller could never manage mandates again).
	if existingSigilID, err := k.sigilKeeper.SigilIDByAddress(ctx, terms.ChamberId, grantee); err == nil {
		existingController, err := k.sigilKeeper.ControllerAddress(ctx, terms.ChamberId, existingSigilID)
		if err == nil && existingController.Equals(grantee) {
			return "", errorsmod.Wrapf(types.ErrGranteeConflict,
				"grantee %s controls sigil %s", grantee, existingSigilID)
		}
	}

	// The new mandate's outstanding_commitment is seeded so that the global
	// invariant Σ outstanding_commitment == Σ charged_spend(live intents) holds
	// the instant the mandate exists — and so the §1.1-goal-3 bound attaches
	// from issuance, never one intent-TTL later:
	//
	//   - REPLACE (the grantee already holds a mandate): delete the superseded
	//     record and INHERIT its outstanding_commitment (D-059). The grantee's
	//     in-flight intents are still live and committed, so the lifetime balance
	//     carries across the reissue (only the per-period window_spent starts
	//     fresh). D-017 retention holds — the 0x02 index is overwritten, never
	//     deleted. Orphans cannot exist on this branch: a mandate-bound key
	//     cannot also submit unmandated root intents (D-017(c)), so every live
	//     intent since the FIRST mandate was already charged into the carried
	//     balance.
	//   - FRESH (no prior mandate): the grantee may already hold LIVE intents it
	//     submitted while unmandated (root/controller ante passthrough — no
	//     charge was ever recorded). ADOPT that live exposure (Σ charged_spend)
	//     into the new mandate (D-064; extends D-059 Option X to the issuance
	//     boundary). charged_spend is UNCHANGED, so DecommitIntent later releases
	//     the SAME figure from this mandate at terminate — adoption and release
	//     are exact inverses. The adoption is CONFINED to this branch on purpose
	//     (TestAdoptionConfinedToFreshBranch guards it): moving it would re-scan
	//     and over-write on every REPLACE/amend, double-counting the carry.
	carriedCommitment := sdk.NewCoins()
	if existing, found, err := k.ActiveMandateByGrantee(ctx, terms.ChamberId, grantee); err != nil {
		return "", err
	} else if found {
		carriedCommitment = existing.OutstandingCommitment
		if err := k.deleteMandate(ctx, existing); err != nil {
			return "", err
		}
	} else if k.liveExposureFn != nil {
		adopted, err := k.liveExposureFn(ctx, terms.ChamberId, grantee)
		if err != nil {
			return "", err
		}
		carriedCommitment = adopted
	}

	seq, err := k.nextIDSequence(ctx)
	if err != nil {
		return "", err
	}
	payload := append(grantee.Bytes(), []byte(terms.SigilId)...)
	id := deriveID(domainMandate, terms.ChamberId, payload, seq)

	m := mandateFromTerms(id, terms, uint64(ctx.BlockHeight()))
	if !carriedCommitment.IsZero() {
		m.OutstandingCommitment = carriedCommitment
	}
	if err := k.SetMandate(ctx, m); err != nil {
		return "", err
	}
	if err := k.setIndexes(ctx, m, grantee); err != nil {
		return "", err
	}
	// register the grantee as a delegate key in the sigil reverse index
	// (SPEC §6.1 key 0x04 "including delegate keys registered via Mandates")
	if err := k.sigilKeeper.SetAddressIndex(ctx, terms.ChamberId, grantee, terms.SigilId); err != nil {
		return "", err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeMandateIssued,
		sdk.NewAttribute(types.AttributeKeyMandateID, id),
		sdk.NewAttribute(types.AttributeKeySigilID, terms.SigilId),
		sdk.NewAttribute(types.AttributeKeyGrantee, terms.GranteeAddr),
		sdk.NewAttribute(types.AttributeKeyController, controller.String()),
	))
	return id, nil
}

// RevokeMandate implements SPEC §6.2 MsgRevokeMandate: controller only,
// effective next block (the revoked flag commits with this block's state;
// every subsequent ante check rejects). The record and its index entries
// are retained (D-017).
func (k Keeper) RevokeMandate(ctx sdk.Context, controller sdk.AccAddress, mandateID string) error {
	m, err := k.MandateByID(ctx, mandateID)
	if err != nil {
		return err
	}
	if err := k.requireController(ctx, m, controller); err != nil {
		return err
	}
	if m.Revoked {
		return errorsmod.Wrapf(types.ErrMandateExpired, "mandate %s already revoked", mandateID)
	}
	m.Revoked = true
	if err := k.SetMandate(ctx, m); err != nil {
		return err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeMandateRevoked,
		sdk.NewAttribute(types.AttributeKeyMandateID, m.Id),
		sdk.NewAttribute(types.AttributeKeySigilID, m.SigilId),
		sdk.NewAttribute(types.AttributeKeyGrantee, m.GranteeAddr),
	))
	return nil
}

// AmendMandate implements SPEC §6.2 MsgAmendMandate: full replacement
// semantics, never in-place mutation of ceilings. The mandate id is stable;
// terms are replaced wholesale and the window accounting resets (D-018).
// Sigil and grantee can never change under amendment — rebinding a mandate
// to a new key is revoke + issue, which preserves D-017 retention for the
// old key.
func (k Keeper) AmendMandate(ctx sdk.Context, controller sdk.AccAddress, mandateID string, terms types.MandateTerms) error {
	if err := terms.Validate(); err != nil {
		return err
	}
	if terms.ExpiryHeight <= uint64(ctx.BlockHeight()) {
		return errorsmod.Wrapf(types.ErrInvalidTerms,
			"expiry_height %d is not beyond current height %d", terms.ExpiryHeight, ctx.BlockHeight())
	}

	m, err := k.MandateByID(ctx, mandateID)
	if err != nil {
		return err
	}
	if m.Revoked {
		return errorsmod.Wrapf(types.ErrMandateExpired,
			"mandate %s is revoked; issue a new mandate instead", mandateID)
	}
	if err := k.requireController(ctx, m, controller); err != nil {
		return err
	}
	if terms.GranteeAddr != m.GranteeAddr {
		return errorsmod.Wrap(types.ErrInvalidTerms, "amendment cannot change the grantee; revoke and issue instead")
	}
	if terms.SigilId != m.SigilId {
		return errorsmod.Wrap(types.ErrInvalidTerms, "amendment cannot change the sigil; revoke and issue instead")
	}
	if terms.ChamberId != m.ChamberId {
		return errorsmod.Wrap(types.ErrInvalidTerms, "amendment cannot change the chamber")
	}

	amended := mandateFromTerms(m.Id, terms, uint64(ctx.BlockHeight()))
	// D-059: an amendment replaces every term wholesale and resets the
	// per-period window_spent (settled budget — fine), but the grantee's
	// still-live intents (grantee/sigil immutable under amendment) remain
	// committed, so their LIFETIME outstanding_commitment carries across the
	// amendment rather than zeroing. This keeps Σ outstanding_commitment ==
	// Σ charged_spend(live intents) true across the controller-initiated reset
	// and prevents the over-commitment the old single-counter reset allowed.
	amended.OutstandingCommitment = m.OutstandingCommitment
	if err := k.SetMandate(ctx, amended); err != nil {
		return err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeMandateAmended,
		sdk.NewAttribute(types.AttributeKeyMandateID, m.Id),
		sdk.NewAttribute(types.AttributeKeySigilID, m.SigilId),
		sdk.NewAttribute(types.AttributeKeyGrantee, m.GranteeAddr),
	))
	return nil
}

// requireController asserts the signer is the current controller of the
// mandate's sigil.
func (k Keeper) requireController(ctx sdk.Context, m types.Mandate, signer sdk.AccAddress) error {
	controller, err := k.sigilKeeper.ControllerAddress(ctx, m.ChamberId, m.SigilId)
	if err != nil {
		return err
	}
	if !controller.Equals(signer) {
		return errorsmod.Wrapf(types.ErrUnauthorized,
			"signer %s is not the controller of sigil %s", signer, m.SigilId)
	}
	return nil
}

// mandateFromTerms assembles a Mandate from controller terms with fresh
// keeper-managed accounting fields (D-004: window_spent,
// window_start_height, revoked are never controller-specified).
func mandateFromTerms(id string, terms types.MandateTerms, height uint64) types.Mandate {
	return types.Mandate{
		Id:                 id,
		SigilId:            terms.SigilId,
		GranteeAddr:        terms.GranteeAddr,
		AllowedMsgTypes:    terms.AllowedMsgTypes,
		SpendCeilingPerTx:  terms.SpendCeilingPerTx,
		SpendCeilingWindow: terms.SpendCeilingWindow,
		WindowBlocks:       terms.WindowBlocks,
		WindowSpent:        sdk.NewCoins(),
		WindowStartHeight:  height,
		// zero by default; the caller overrides it to carry the grantee's
		// still-live intent commitments — amend/REPLACE inherit the prior
		// balance (D-059); a FRESH issuance ADOPTS any live root-era exposure
		// (D-064) — since those funds stay committed across the term change.
		OutstandingCommitment: sdk.NewCoins(),
		CounterpartyClass:     terms.CounterpartyClass,
		FeeSponsor:            terms.FeeSponsor,
		FeeDenoms:             terms.FeeDenoms,
		ExpiryHeight:          terms.ExpiryHeight,
		Revoked:               false,
		ChamberId:             terms.ChamberId,
	}
}

// decodeSigilID decodes a 64-char hex sigil id into raw bytes for index keys.
func decodeSigilID(id string) ([]byte, error) {
	bz, err := types.MandateIDBytes(id) // same 32-byte hex shape (D-009)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrInvalidTerms, "sigil_id: %s", err)
	}
	return bz, nil
}
