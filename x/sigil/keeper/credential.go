package keeper

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

// SetCredential stores a credential anchor.
// Implements SPEC §6.1 (key 0x02|chamber|sigil_id|cred_id).
func (k Keeper) SetCredential(ctx sdk.Context, cred types.CredentialAnchor) error {
	sigilIDBz, err := types.SigilIDBytes(cred.SigilId)
	if err != nil {
		return err
	}
	credIDBz, err := types.SigilIDBytes(cred.Id)
	if err != nil {
		return err
	}
	bz, err := k.cdc.Marshal(&cred)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(types.CredentialKey(cred.ChamberId, sigilIDBz, credIDBz), bz)
}

// GetCredential returns a credential anchor by subject sigil and id.
func (k Keeper) GetCredential(ctx sdk.Context, chamberID uint32, sigilID, credID string) (types.CredentialAnchor, error) {
	sigilIDBz, err := types.SigilIDBytes(sigilID)
	if err != nil {
		return types.CredentialAnchor{}, err
	}
	credIDBz, err := types.SigilIDBytes(credID)
	if err != nil {
		return types.CredentialAnchor{}, err
	}
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.CredentialKey(chamberID, sigilIDBz, credIDBz))
	if err != nil {
		return types.CredentialAnchor{}, err
	}
	if bz == nil {
		return types.CredentialAnchor{}, errorsmod.Wrapf(types.ErrCredentialNotFound, "id %s", credID)
	}
	var cred types.CredentialAnchor
	if err := k.cdc.Unmarshal(bz, &cred); err != nil {
		return types.CredentialAnchor{}, err
	}
	return cred, nil
}

// AnchorCredential anchors a credential to a subject sigil. The issuer
// address must be the controller of issuerSigil. Implements SPEC §6.1
// MsgAnchorCredential.
func (k Keeper) AnchorCredential(
	ctx sdk.Context,
	issuer sdk.AccAddress,
	issuerSigilID, subjectSigilID, schemaID string,
	claimHash []byte,
	expiryHeight uint64,
	chamberID uint32,
) (string, error) {
	issuerSigil, err := k.GetSigil(ctx, chamberID, issuerSigilID)
	if err != nil {
		return "", err
	}
	if issuerSigil.ControllerAddr != issuer.String() {
		return "", errorsmod.Wrapf(types.ErrUnauthorized,
			"%s is not the controller of issuer sigil %s", issuer, issuerSigilID)
	}
	if _, err := k.GetSigil(ctx, chamberID, subjectSigilID); err != nil {
		return "", err
	}
	if expiryHeight != 0 && expiryHeight <= uint64(ctx.BlockHeight()) {
		return "", errorsmod.Wrapf(types.ErrInvalidExpiry,
			"expiry %d not after current height %d", expiryHeight, ctx.BlockHeight())
	}

	seq, err := k.nextIDSequence(ctx)
	if err != nil {
		return "", err
	}
	payload := append([]byte(issuerSigilID+subjectSigilID+schemaID), claimHash...)
	id := deriveID(0x02, chamberID, payload, seq)

	cred := types.CredentialAnchor{
		Id:           id,
		SigilId:      subjectSigilID,
		IssuerSigil:  issuerSigilID,
		SchemaId:     schemaID,
		ClaimHash:    claimHash,
		ExpiryHeight: expiryHeight,
		Revoked:      false,
		ChamberId:    chamberID,
	}
	if err := k.SetCredential(ctx, cred); err != nil {
		return "", err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeAnchorCredential,
		sdk.NewAttribute(types.AttributeKeyCredentialID, id),
		sdk.NewAttribute(types.AttributeKeySigilID, subjectSigilID),
		sdk.NewAttribute(types.AttributeKeyIssuerSigil, issuerSigilID),
	))
	return id, nil
}

// RevokeCredential marks a credential revoked. Only the controller of the
// issuing sigil may revoke (SPEC §6.1 "issuer only").
func (k Keeper) RevokeCredential(ctx sdk.Context, issuer sdk.AccAddress, chamberID uint32, subjectSigilID, credID string) error {
	cred, err := k.GetCredential(ctx, chamberID, subjectSigilID, credID)
	if err != nil {
		return err
	}
	if cred.Revoked {
		return errorsmod.Wrapf(types.ErrCredentialRevoked, "id %s", credID)
	}

	issuerSigil, err := k.GetSigil(ctx, chamberID, cred.IssuerSigil)
	if err != nil {
		return err
	}
	if issuerSigil.ControllerAddr != issuer.String() {
		return errorsmod.Wrapf(types.ErrUnauthorized,
			"%s is not the controller of issuer sigil %s", issuer, cred.IssuerSigil)
	}

	cred.Revoked = true
	if err := k.SetCredential(ctx, cred); err != nil {
		return err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRevokeCredential,
		sdk.NewAttribute(types.AttributeKeyCredentialID, credID),
		sdk.NewAttribute(types.AttributeKeySigilID, subjectSigilID),
	))
	return nil
}

// IsCredentialActive reports whether a credential may satisfy
// counterparty-class predicates: it must exist, not be revoked, and not be
// expired. This is the accessor x/session's CEL evaluation must use
// (SPEC §6.1 invariant 3; CEL wiring lands at task 0.8).
func (k Keeper) IsCredentialActive(ctx sdk.Context, chamberID uint32, sigilID, credID string) bool {
	cred, err := k.GetCredential(ctx, chamberID, sigilID, credID)
	if err != nil {
		return false
	}
	if cred.Revoked {
		return false
	}
	if cred.ExpiryHeight != 0 && cred.ExpiryHeight <= uint64(ctx.BlockHeight()) {
		return false
	}
	return true
}

// IterateCredentials walks all credentials of a sigil.
func (k Keeper) IterateCredentials(ctx sdk.Context, chamberID uint32, sigilID string, cb func(types.CredentialAnchor) bool) error {
	sigilIDBz, err := types.SigilIDBytes(sigilID)
	if err != nil {
		return err
	}
	prefix := types.CredentialIterPrefix(chamberID, sigilIDBz)
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(prefix, append(prefix, 0xFF))
	if err != nil {
		return err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var cred types.CredentialAnchor
		if err := k.cdc.Unmarshal(it.Value(), &cred); err != nil {
			return err
		}
		if cb(cred) {
			break
		}
	}
	return nil
}

// IterateAllCredentials walks every credential in the store (invariants).
func (k Keeper) IterateAllCredentials(ctx sdk.Context, cb func(types.CredentialAnchor) bool) error {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.CredentialKeyPrefix, append(types.CredentialKeyPrefix, 0xFF))
	if err != nil {
		return err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var cred types.CredentialAnchor
		if err := k.cdc.Unmarshal(it.Value(), &cred); err != nil {
			return err
		}
		if cb(cred) {
			break
		}
	}
	return nil
}
