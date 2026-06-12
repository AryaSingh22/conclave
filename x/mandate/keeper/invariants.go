package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// RegisterInvariants registers the x/mandate invariants with the crisis
// module. The grantee-resolves-to-sigil invariant is the cross-module
// statement SPEC §6.1 lists ("every Mandate's agent key resolves to exactly
// one Sigil"), deferred from task 0.3 and registered here where both
// keepers are in scope.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "grantee-resolves-to-sigil", GranteeResolvesToSigilInvariant(k))
	ir.RegisterRoute(types.ModuleName, "grantee-index", GranteeIndexInvariant(k))
}

// GranteeResolvesToSigilInvariant implements SPEC §6.1 invariant 1, mandate
// side: every Mandate's grantee key resolves through the sigil reverse index
// (0x04) to exactly the Sigil the mandate names, and that Sigil exists.
func GranteeResolvesToSigilInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msg string
		broken := false

		if err := k.IterateMandates(ctx, func(m types.Mandate) bool {
			grantee, err := sdk.AccAddressFromBech32(m.GranteeAddr)
			if err != nil {
				broken = true
				msg += fmt.Sprintf("mandate %s grantee %q unparseable\n", m.Id, m.GranteeAddr)
				return false
			}
			indexed, err := k.sigilKeeper.SigilIDByAddress(ctx, m.ChamberId, grantee)
			if err != nil {
				broken = true
				msg += fmt.Sprintf("mandate %s grantee %s resolves to no sigil\n", m.Id, m.GranteeAddr)
				return false
			}
			if indexed != m.SigilId {
				broken = true
				msg += fmt.Sprintf("mandate %s grantee %s resolves to sigil %s, mandate names %s\n",
					m.Id, m.GranteeAddr, indexed, m.SigilId)
				return false
			}
			if _, err := k.sigilKeeper.ControllerAddress(ctx, m.ChamberId, m.SigilId); err != nil {
				broken = true
				msg += fmt.Sprintf("mandate %s names missing sigil %s\n", m.Id, m.SigilId)
			}
			return false
		}); err != nil {
			return sdk.FormatInvariant(types.ModuleName, "grantee-resolves-to-sigil", err.Error()), true
		}

		return sdk.FormatInvariant(types.ModuleName, "grantee-resolves-to-sigil", msg), broken
	}
}

// GranteeIndexInvariant asserts the SPEC §6.2 0x02 index and the 0x01
// records are mutually consistent: every index entry points to an existing
// mandate for that grantee, and every mandate is its grantee's indexed
// mandate (one active mandate per key, D2 REPLACE semantics).
func GranteeIndexInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msg string
		broken := false

		if err := k.IterateGranteeIndex(ctx, func(chamberID uint32, grantee sdk.AccAddress, mandateIDBz []byte) bool {
			m, found, err := k.ActiveMandateByGrantee(ctx, chamberID, grantee)
			if err != nil || !found {
				broken = true
				msg += fmt.Sprintf("index entry %s → %x points to missing mandate\n", grantee, mandateIDBz)
				return false
			}
			if m.GranteeAddr != grantee.String() {
				broken = true
				msg += fmt.Sprintf("mandate %s indexed under %s but names grantee %s\n", m.Id, grantee, m.GranteeAddr)
			}
			return false
		}); err != nil {
			return sdk.FormatInvariant(types.ModuleName, "grantee-index", err.Error()), true
		}

		if err := k.IterateMandates(ctx, func(m types.Mandate) bool {
			grantee := sdk.MustAccAddressFromBech32(m.GranteeAddr)
			indexed, found, err := k.ActiveMandateByGrantee(ctx, m.ChamberId, grantee)
			if err != nil || !found || indexed.Id != m.Id {
				broken = true
				msg += fmt.Sprintf("mandate %s is not its grantee's indexed mandate\n", m.Id)
			}
			return false
		}); err != nil {
			return sdk.FormatInvariant(types.ModuleName, "grantee-index", err.Error()), true
		}

		return sdk.FormatInvariant(types.ModuleName, "grantee-index", msg), broken
	}
}
