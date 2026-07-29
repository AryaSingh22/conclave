package types_test

import (
	"errors"
	"testing"

	errorsmod "cosmossdk.io/errors"
	"github.com/stretchr/testify/require"

	"github.com/AryaSingh22/conclave/x/session/types"
)

// TestInvalidSolutionSurfacesRegisteredCode is the load-bearing gate for D-091
// finding F-1 (task 2.13): ProposeSolution's rejection must reach the wire as
// the REGISTERED session code 1412, not the internal code 1.
//
// errorsmod.ABCIInfo is not a proxy for the wire path — it IS the wire path:
// baseapp builds a failed tx result with sdkerrors.ResponseExecTxResultWithEvents,
// whose whole body is `space, code, log := errorsmod.ABCIInfo(err, debug)`
// (cosmos-sdk v0.50.15 types/errors/abci.go). So asserting on ABCIInfo asserts
// exactly what a client receives.
//
// The pre-2.13 composition — errors.Join(ErrInvalidSolution, rule) — fails this
// test with code 1 and an empty codespace (see
// TestErrorsJoinLosesTheRegisteredCode below, which pins WHY).
func TestInvalidSolutionSurfacesRegisteredCode(t *testing.T) {
	// One case per shape of §6.4 rule error a solution can be rejected for, so a
	// pass is not an accident of one rule's wrapping.
	cases := []struct {
		name string
		rule error
	}{
		{"consent gate (D-075/D-076)", errorsmod.Wrapf(types.ErrConditionNotConsented, "leg %d: buyer %s did not sign this ZK condition", 0, "sigil-b")},
		{"limit rate (D-089)", errorsmod.Wrap(types.ErrLimitRateUnsupported, "counter-side carries 2 distinct assets")},
		{"conservation", errorsmod.Wrap(types.ErrConservation, "asset uusdc given 5 != received 4")},
		{"intent not open", errorsmod.Wrapf(types.ErrIntentNotOpen, "intent %s not in session %d", "abc", 7)},
		{"bare sentinel, unwrapped", types.ErrDuplicateSigil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := types.InvalidSolution(tc.rule)

			// 1. THE FIX: the registered code and codespace reach ABCI.
			space, code, log := errorsmod.ABCIInfo(err, false)
			require.Equal(t, types.ErrInvalidSolution.ABCICode(), code,
				"the registered session code must survive to the wire (D-091 F-1)")
			require.Equal(t, uint32(1412), code, "SPEC §13: session codespace is 14xx and ErrInvalidSolution is 1412")
			require.Equal(t, types.ModuleName, space,
				"(codespace, code) is the client's dispatch key — both halves must be the registered ones")

			// 2. The rule error stays READABLE in the log: a solver must still be
			// able to tell WHICH §6.4 rule rejected it.
			require.Contains(t, log, tc.rule.Error(),
				"the specific rule error must remain in the reason text")
			require.Contains(t, log, types.ErrInvalidSolution.Error())

			// 3. Both identities stay reachable programmatically — this is what
			// errors.Join bought and what a flattening errorsmod.Wrap would lose.
			require.ErrorIs(t, err, types.ErrInvalidSolution)
			require.ErrorIs(t, err, tc.rule)
		})
	}
}

// TestInvalidSolutionDoesNotMatchUnrelatedErrors guards the other direction: the
// composite must not make errors.Is true for a rule that did NOT fire. Without
// it, assertion 3 above would pass against a value that matches everything.
func TestInvalidSolutionDoesNotMatchUnrelatedErrors(t *testing.T) {
	err := types.InvalidSolution(errorsmod.Wrap(types.ErrConservation, "asset uusdc given 5 != received 4"))

	require.NotErrorIs(t, err, types.ErrConditionNotConsented)
	require.NotErrorIs(t, err, types.ErrLimitRateUnsupported)
	require.NotErrorIs(t, err, types.ErrSolverNotActive)
}

// TestErrorsJoinLosesTheRegisteredCode pins the LIBRARY BEHAVIOUR that made
// D-091's F-1 possible, so the defect cannot silently return: if a future edit
// reaches for errors.Join again (it reads like the natural composition — it
// keeps both errors.Is directions), this test documents, in executable form,
// exactly what it costs on the wire.
//
// cosmossdk.io/errors v1.0.1 extracts the ABCI code by walking coder→causer
// only; a multi-error exposes Unwrap() []error and satisfies neither, so
// extraction falls through to internalABCICode = 1 / UndefinedCodespace.
func TestErrorsJoinLosesTheRegisteredCode(t *testing.T) {
	rule := errorsmod.Wrap(types.ErrConditionNotConsented, "seller did not sign this condition")
	joined := errors.Join(types.ErrInvalidSolution, rule)

	space, code, _ := errorsmod.ABCIInfo(joined, false)
	require.Equal(t, uint32(1), code, "errors.Join drops the registered code to the internal 1")
	require.Equal(t, errorsmod.UndefinedCodespace, space, "and drops the codespace with it")

	// And this is WHY the bug survived a suite full of green assertions: the
	// errors.Is properties every existing test checks hold under BOTH shapes.
	require.ErrorIs(t, joined, types.ErrInvalidSolution)
	require.ErrorIs(t, joined, types.ErrConditionNotConsented)
}
