package keeper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

// hexID renders a 32-byte id whose FIRST byte is `first` — the byte that
// decides whether a record sorts inside or outside a prefix‖0xFF range.
func hexID(first string) string {
	return first + strings.Repeat("b2", 31)
}

// TestHasActiveCredentialSchemaFindsHighByteCredID is the D-088 CONSENSUS-path
// regression. IterateCredentials backs HasActiveCredentialSchema, which backs
// the SPEC §6.4 rule-5 counterparty-class predicate has_credential(schema) —
// so a credential the walk cannot see is a credential the matcher denies.
//
// WATCHED RED against the pre-fix code: with `append(prefix, 0xFF)` the
// cred_id 0xFF… record is skipped and the predicate answers FALSE for a sigil
// that genuinely holds the credential, wrongly rejecting a valid match. Every
// node computes the same wrong answer (deterministic — no fork, no halt), so
// the defect is a silent correctness/liveness loss, not a safety break.
func TestHasActiveCredentialSchemaFindsHighByteCredID(t *testing.T) {
	ctx, k := setupKeeper(t)
	subject := hexID("0d")

	cred := types.CredentialAnchor{
		Id:       hexID("ff"),
		SigilId:  subject,
		SchemaId: "kyc",
	}
	require.NoError(t, k.SetCredential(ctx, cred))

	require.True(t, k.HasActiveCredentialSchema(ctx, 0, subject, "kyc"),
		"credential with id 0xFF… is invisible to the per-sigil walk, so the rule-5 predicate wrongly denies a credentialed party (D-088)")
}

// TestIterateCredentialsFindsHighByteCredID proves the same omission at the
// iterator itself, and that a low-byte control is unaffected (the walk is
// selectively truncating, not broken outright).
func TestIterateCredentialsFindsHighByteCredID(t *testing.T) {
	ctx, k := setupKeeper(t)
	subject := hexID("0e")

	for _, first := range []string{"00", "ff"} {
		require.NoError(t, k.SetCredential(ctx, types.CredentialAnchor{
			Id:       hexID(first),
			SigilId:  subject,
			SchemaId: "kyc",
		}))
	}

	var seen int
	require.NoError(t, k.IterateCredentials(ctx, 0, subject, func(types.CredentialAnchor) bool {
		seen++
		return false
	}))
	require.Equal(t, 2, seen,
		"the 0xFF-prefixed credential is truncated out of the range (D-088)")
}
