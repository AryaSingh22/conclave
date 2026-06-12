package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/x/feegrant"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// ForbiddenMsgTypes returns the msg TypeURLs that can never appear in
// allowed_msg_types — the SPEC §6.2 step 3 privilege-escalation guard,
// enforced at MsgIssueMandate/MsgAmendMandate validation.
//
// Beyond the mandate lifecycle msgs the spec names, authz MsgGrant and
// feegrant MsgGrantAllowance are forbidden (D-016): a mandate-bound key
// permitted to MsgGrant authz over its own account would create an unbound
// delegate key whose MsgExec txs are signed by that unbound key — step 1
// passes the signer as a root key, bypassing the mandate entirely.
func ForbiddenMsgTypes() []string {
	return []string{
		sdk.MsgTypeURL(&MsgIssueMandate{}),
		sdk.MsgTypeURL(&MsgRevokeMandate{}),
		sdk.MsgTypeURL(&MsgAmendMandate{}),
		sdk.MsgTypeURL(&MsgUpdateParams{}),
		sdk.MsgTypeURL(&authz.MsgGrant{}),
		sdk.MsgTypeURL(&feegrant.MsgGrantAllowance{}),
	}
}

// Validate performs stateless validation of controller-specified mandate
// terms (SPEC §6.2; the keeper adds stateful checks: controller resolution,
// expiry vs current height, grantee conflicts).
func (t MandateTerms) Validate() error {
	if _, err := sdk.AccAddressFromBech32(t.GranteeAddr); err != nil {
		return errorsmod.Wrapf(ErrInvalidTerms, "grantee_addr: %s", err)
	}
	if len(t.SigilId) != 2*IDLength {
		return errorsmod.Wrapf(ErrInvalidTerms, "sigil_id %q is not a 64-char hex id", t.SigilId)
	}

	forbidden := make(map[string]bool, 6)
	for _, url := range ForbiddenMsgTypes() {
		forbidden[url] = true
	}
	seen := make(map[string]bool, len(t.AllowedMsgTypes))
	for _, url := range t.AllowedMsgTypes {
		if url == "" {
			return errorsmod.Wrap(ErrInvalidTerms, "empty allowed msg type")
		}
		if forbidden[url] {
			return errorsmod.Wrapf(ErrPrivilegeEscalation, "%s", url)
		}
		if seen[url] {
			return errorsmod.Wrapf(ErrInvalidTerms, "duplicate allowed msg type %s", url)
		}
		seen[url] = true
	}

	if err := t.SpendCeilingPerTx.Validate(); err != nil {
		return errorsmod.Wrapf(ErrInvalidTerms, "spend_ceiling_per_tx: %s", err)
	}
	if err := t.SpendCeilingWindow.Validate(); err != nil {
		return errorsmod.Wrapf(ErrInvalidTerms, "spend_ceiling_window: %s", err)
	}
	if !t.SpendCeilingWindow.IsZero() && t.WindowBlocks == 0 {
		return errorsmod.Wrap(ErrInvalidTerms, "window_blocks must be positive when spend_ceiling_window is set")
	}

	if t.FeeSponsor != "" {
		if _, err := sdk.AccAddressFromBech32(t.FeeSponsor); err != nil {
			return errorsmod.Wrapf(ErrInvalidTerms, "fee_sponsor: %s", err)
		}
	}
	seenDenom := make(map[string]bool, len(t.FeeDenoms))
	for _, denom := range t.FeeDenoms {
		if err := sdk.ValidateDenom(denom); err != nil {
			return errorsmod.Wrapf(ErrInvalidTerms, "fee_denoms: %s", err)
		}
		if seenDenom[denom] {
			return errorsmod.Wrapf(ErrInvalidTerms, "duplicate fee denom %s", denom)
		}
		seenDenom[denom] = true
	}

	if t.ExpiryHeight == 0 {
		return errorsmod.Wrap(ErrInvalidTerms, "expiry_height must be positive")
	}
	return nil
}
