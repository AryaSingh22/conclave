package types

import (
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"cosmossdk.io/math"
)

type fakeEnv struct {
	rep   math.LegacyDec
	creds map[string]bool
}

func (e fakeEnv) Reputation() math.LegacyDec  { return e.rep }
func (e fakeEnv) HasCredential(s string) bool { return e.creds[s] }

func TestParsePredicate_Units(t *testing.T) {
	valid := []string{
		"",
		"reputation >= 50",
		"reputation>50",
		"reputation < 100 && reputation >= 10",
		"reputation >= 10 || reputation == 0",
		"has_credential(\"kyc\")",
		"reputation >= 50 && has_credential(\"kyc\")",
		"(reputation >= 10 || reputation < 5) && has_credential(\"audit\")",
		"reputation != 0",
		"reputation >= 12.5",
	}
	for _, s := range valid {
		t.Run("ok/"+s, func(t *testing.T) {
			_, err := ParsePredicate(s)
			require.NoError(t, err)
		})
	}

	malformed := []string{
		"reputation",
		"reputation >=",
		"reputation >= abc",
		"reputation >> 5",
		"foo >= 5",
		"has_credential()",
		"has_credential(kyc)", // unquoted
		"has_credential(\"\")",
		"reputation >= 5 &&",
		"&& reputation >= 5",
		"(reputation >= 5",
		"reputation >= 5)",
		"reputation = 5", // single '=' not allowed
		"reputation >= 5 | reputation < 1",
		"reputation >= 5 & reputation < 1",
	}
	for _, s := range malformed {
		t.Run("bad/"+s, func(t *testing.T) {
			_, err := ParsePredicate(s)
			require.Error(t, err, "malformed predicate must fail to parse (fail-closed): %q", s)
		})
	}
}

// TestEmptyIsNotMalformed pins Decision A: empty is always-true, malformed is
// rejected — they are distinct.
func TestEmptyIsNotMalformed(t *testing.T) {
	empty, err := ParsePredicate("   ")
	require.NoError(t, err)
	require.True(t, empty.IsEmpty())
	require.True(t, empty.Eval(fakeEnv{rep: math.LegacyZeroDec()}))

	_, err = ParsePredicate("garbage")
	require.Error(t, err)
}

func TestPredicate_Eval(t *testing.T) {
	env := fakeEnv{rep: math.LegacyNewDec(50), creds: map[string]bool{"kyc": true}}
	cases := []struct {
		pred string
		want bool
	}{
		{"", true},
		{"reputation >= 50", true},
		{"reputation > 50", false},
		{"reputation < 100", true},
		{"reputation == 50", true},
		{"reputation != 50", false},
		{"has_credential(\"kyc\")", true},
		{"has_credential(\"audit\")", false},
		{"reputation >= 50 && has_credential(\"kyc\")", true},
		{"reputation >= 50 && has_credential(\"audit\")", false},
		{"reputation > 99 || has_credential(\"kyc\")", true},
		{"reputation > 99 || has_credential(\"audit\")", false},
		{"(reputation >= 10 || reputation < 5) && has_credential(\"kyc\")", true},
	}
	for _, tc := range cases {
		t.Run(tc.pred, func(t *testing.T) {
			p, err := ParsePredicate(tc.pred)
			require.NoError(t, err)
			require.Equal(t, tc.want, p.Eval(env))
		})
	}
}

// TestParsePredicate_Fuzz drives arbitrary bytes through the parser (Decision A):
// parsing never panics and is deterministic across runs; an accepted predicate
// evaluates without panic and deterministically. Garbage fails closed.
func TestParsePredicate_Fuzz(t *testing.T) {
	tokens := []string{"reputation", "has_credential", "(", ")", "&&", "||", ">=", "<=", "==", "!=", ">", "<", "50", "12.5", "\"kyc\"", " ", "x", "="}
	env := fakeEnv{rep: math.LegacyNewDec(42), creds: map[string]bool{"kyc": true}}

	rapid.Check(t, func(rt *rapid.T) {
		var s string
		if rapid.Bool().Draw(rt, "structured") {
			n := rapid.IntRange(0, 12).Draw(rt, "n")
			for i := 0; i < n; i++ {
				s += rapid.SampledFrom(tokens).Draw(rt, "tok")
			}
		} else {
			s = rapid.String().Draw(rt, "raw")
		}

		p1, err1 := ParsePredicate(s)
		p2, err2 := ParsePredicate(s)
		require.Equal(rt, err1 == nil, err2 == nil, "non-deterministic parse of %q", s)
		if err1 != nil {
			return
		}
		// accepted predicate: evaluation never panics and is deterministic.
		require.Equal(rt, p1.Eval(env), p2.Eval(env), "non-deterministic eval of %q", s)
	})
}
