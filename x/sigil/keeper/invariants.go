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
// correctly indexed. The mandate side of the invariant ("every Mandate's
// agent key resolves to exactly one Sigil") is registered by x/mandate as
// grantee-resolves-to-sigil, where both keepers are in scope (task 0.4).
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
// revoked credentials never satisfy counterparty-class predicates. This
// invariant asserts that IsCredentialActive never reports a revoked credential
// active.
//
// MARKER RETIRED at PHASE2_DESIGN Rev 3 (D-090). The former phase-0 marker
// said "task 0.8 wires CEL counterparty-class
// evaluation through IsCredentialActive; this invariant then covers the full
// chain." Task 0.8 DID wire the chain — `x/session`'s predicate environment
// calls HasActiveCredentialSchema, which walks credentials and gates on
// IsCredentialActive, backing the SPEC §6.4 rule-5 `has_credential(schema)`
// predicate — so this invariant now covers it end to end. The one word that
// was wrong is "CEL": the evaluator is a Go predicate environment, not a CEL
// interpreter (no CEL library is imported anywhere in the tree). The wiring
// landed; only the mechanism differed, and a completed decision must not keep
// wearing a TODO's clothing (AUDIT F-8's class).
//
// That chain is load-bearing and has already failed once: task 2.11 (D-088)
// found IterateCredentials silently dropping 1-in-256 credentials through a
// truncated iterator range end, so a genuinely credentialed party was wrongly
// rejected by rule 5. Deterministic across nodes, hence no fork — but a real
// correctness loss on a consensus path this invariant does not detect (the
// dropped record is invisible to the walk, so it looks like "no such record").
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
