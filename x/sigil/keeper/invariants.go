package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

// RegisterInvariants registers the x/sigil invariants with the crisis module
// (SPEC §6.1 "Invariants (register with crisis module)").
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "address-index", AddressIndexInvariant(k))
	ir.RegisterRoute(types.ModuleName, "reputation-non-negative", ReputationNonNegativeInvariant(k))
	ir.RegisterRoute(types.ModuleName, "revoked-credentials-inactive", RevokedCredentialsInactiveInvariant(k))
}

// AddressIndexInvariant implements SPEC §6.1 invariant 1: every indexed key
// resolves to exactly one existing Sigil, and every sigil's controller is
// correctly indexed.
//
// CONCLAVE-TODO(phase-0): task 0.4 extends this to assert every Mandate's
// grantee key resolves through this index ("every Mandate's agent key
// resolves to exactly one Sigil").
func AddressIndexInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msg string
		broken := false

		if err := k.IterateAddressIndex(ctx, func(addr sdk.AccAddress, sigilID string, chamberID uint32) bool {
			if _, err := k.GetSigil(ctx, chamberID, sigilID); err != nil {
				broken = true
				msg += fmt.Sprintf("index entry %s → %s points to missing sigil\n", addr, sigilID)
			}
			return false
		}); err != nil {
			return sdk.FormatInvariant(types.ModuleName, "address-index", err.Error()), true
		}

		if err := k.IterateSigils(ctx, func(s types.Sigil) bool {
			controller := sdk.MustAccAddressFromBech32(s.ControllerAddr)
			indexed, err := k.SigilIDByAddress(ctx, s.ChamberId, controller)
			if err != nil || indexed != s.Id {
				broken = true
				msg += fmt.Sprintf("sigil %s controller %s not correctly indexed\n", s.Id, s.ControllerAddr)
			}
			return false
		}); err != nil {
			return sdk.FormatInvariant(types.ModuleName, "address-index", err.Error()), true
		}

		return sdk.FormatInvariant(types.ModuleName, "address-index", msg), broken
	}
}

// ReputationNonNegativeInvariant implements SPEC §6.1 invariant 2:
// reputation never negative.
func ReputationNonNegativeInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msg string
		broken := false
		if err := k.IterateReputations(ctx, func(r types.Reputation) bool {
			if r.Score.IsNil() || r.Score.IsNegative() {
				broken = true
				msg += fmt.Sprintf("sigil %s has negative reputation %s\n", r.SigilId, r.Score)
			}
			return false
		}); err != nil {
			return sdk.FormatInvariant(types.ModuleName, "reputation-non-negative", err.Error()), true
		}
		return sdk.FormatInvariant(types.ModuleName, "reputation-non-negative", msg), broken
	}
}

// RevokedCredentialsInactiveInvariant implements SPEC §6.1 invariant 3:
// revoked credentials never satisfy counterparty-class predicates. The
// predicate evaluator (x/session, task 0.8) must gate on IsCredentialActive;
// this invariant asserts that accessor never reports a revoked credential
// active.
//
// CONCLAVE-TODO(phase-0): task 0.8 wires CEL counterparty-class evaluation
// through IsCredentialActive; this invariant then covers the full chain.
func RevokedCredentialsInactiveInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msg string
		broken := false
		if err := k.IterateAllCredentials(ctx, func(c types.CredentialAnchor) bool {
			if c.Revoked && k.IsCredentialActive(ctx, c.ChamberId, c.SigilId, c.Id) {
				broken = true
				msg += fmt.Sprintf("revoked credential %s reported active\n", c.Id)
			}
			return false
		}); err != nil {
			return sdk.FormatInvariant(types.ModuleName, "revoked-credentials-inactive", err.Error()), true
		}
		return sdk.FormatInvariant(types.ModuleName, "revoked-credentials-inactive", msg), broken
	}
}
