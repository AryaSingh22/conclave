package types

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
)

// x/session error registry. Stable codespace per SPEC §13: session = 14xx.
var (
	ErrInvalidID                 = errorsmod.Register(ModuleName, 1400, "invalid id")
	ErrInvalidParams             = errorsmod.Register(ModuleName, 1401, "invalid params")
	ErrUnauthorized              = errorsmod.Register(ModuleName, 1402, "unauthorized")
	ErrSessionNotFound           = errorsmod.Register(ModuleName, 1403, "session not found")
	ErrSolverNotFound            = errorsmod.Register(ModuleName, 1404, "solver not found")
	ErrSolverExists              = errorsmod.Register(ModuleName, 1405, "solver already registered")
	ErrBondInsufficient          = errorsmod.Register(ModuleName, 1406, "solver bond below minimum")
	ErrInvalidBond               = errorsmod.Register(ModuleName, 1407, "invalid solver bond")
	ErrSolverNotActive           = errorsmod.Register(ModuleName, 1408, "solver is not active")
	ErrSessionNotInAuction       = errorsmod.Register(ModuleName, 1409, "session is not accepting solutions")
	ErrEquivocation              = errorsmod.Register(ModuleName, 1410, "solver already proposed for this session")
	ErrTooManySolutions          = errorsmod.Register(ModuleName, 1411, "session solution limit reached")
	ErrInvalidSolution           = errorsmod.Register(ModuleName, 1412, "solution failed validation")
	ErrIntentNotOpen             = errorsmod.Register(ModuleName, 1413, "referenced intent is not OPEN in this session")
	ErrIntentReused              = errorsmod.Register(ModuleName, 1414, "intent referenced in two matches")
	ErrConservation              = errorsmod.Register(ModuleName, 1415, "match graph does not balance (an asset is given != received)")
	ErrLimitViolation            = errorsmod.Register(ModuleName, 1416, "match violates a party's give/want limit")
	ErrPriceMismatch             = errorsmod.Register(ModuleName, 1424, "leg amounts do not clear at the stated prices")
	ErrNonUniformPrice           = errorsmod.Register(ModuleName, 1417, "asset pair clears at more than one price")
	ErrCounterpartyClass         = errorsmod.Register(ModuleName, 1418, "counterparty-class predicate not satisfied")
	ErrGraphTooLarge             = errorsmod.Register(ModuleName, 1419, "settlement graph exceeds max_graph_legs")
	ErrConditionsNotEnabled      = errorsmod.Register(ModuleName, 1420, "delivery conditions are disabled in Phase 0")
	ErrDuplicateSigil            = errorsmod.Register(ModuleName, 1421, "a sigil appears on two intents in one match")
	ErrConcludeFailed            = errorsmod.Register(ModuleName, 1422, "settlement graph execution failed")
	ErrInvalidLeg                = errorsmod.Register(ModuleName, 1423, "invalid settlement leg")
	ErrSkimViolation             = errorsmod.Register(ModuleName, 1425, "surplus skim violates the residual==-skim binding (would touch principal or mis-route)")
	ErrConditionUnsupportedMatch = errorsmod.Register(ModuleName, 1426, "conditioned (HASHLOCK/OPTIMISTIC) leg requires an unambiguous bilateral match: one DataGrant leg seller→buyer plus coin payment legs buyer→seller, exactly two sigils")
	ErrConditionNotConsented     = errorsmod.Register(ModuleName, 1427, "settlement leg carries a delivery condition the seller did not sign (SettleLeg.condition must equal the seller intent's signed IntentPayload.condition — a solver cannot impose a condition)")
	ErrLimitRateUnsupported      = errorsmod.Register(ModuleName, 1428, "a leg's signed §5.1 limit RATE cannot be evaluated for this match shape (no single counter-asset, a malformed/negative limit, or operands beyond exact comparison) — the match is rejected rather than settled against an unhonored limit")
)

// InvalidSolution composes the registered ErrInvalidSolution sentinel with the
// SPEC §6.4 rule error that rejected the solution, so that BOTH survive:
//
//   - the ABCI layer reads the registered session code 1412 and codespace
//     "session" off the returned value (SPEC §13's stable-codespace contract),
//     so an off-chain solver can dispatch on the documented code; and
//   - errors.Is finds the sentinel AND the specific rule error, so in-process
//     callers and tests can assert the precise §6.4 rule that failed.
//
// It exists because neither composition the standard libraries offer keeps both
// (D-091 finding F-1, task 2.13): errors.Join yields a value implementing only
// Unwrap() []error, and cosmossdk.io/errors extracts the ABCI code by walking
// coder→causer ONLY (see its abci.go abciCode/abciCodespace) — it cannot
// traverse a multi-error, so a joined error falls through to the internal code
// 1 with an empty codespace and every invalid solution looks like an internal
// error on the wire. Conversely errorsmod.Wrap(ErrInvalidSolution, rule.Error())
// restores the code but flattens the rule error to text, losing errors.Is.
//
// The code deliberately stays the message-level 1412 rather than the rule's own
// code: 1412 is the stable "your solution was rejected" identity, and per-rule
// sub-codes can be added later WITHOUT breaking a client that dispatches on
// 1412 — not the other way round. The rule's identity remains available in the
// log text and via errors.Is.
//
// NOTE (SPEC §11.6): a tx result code is consensus data — CometBFT hashes
// {Code, Data, GasWanted, GasUsed} into LastResultsHash — so this value is
// state-machine-visible on a live network and must be deterministic. It is: a
// pure function of the rule error, with no map iteration, clock, or float.
func InvalidSolution(rule error) error {
	if rule == nil {
		return ErrInvalidSolution
	}
	return invalidSolutionError{rule: rule}
}

// invalidSolutionError is InvalidSolution's carrier. It implements
// cosmossdk.io/errors' unexported `coder` and `codespacer` interfaces directly
// (rather than relying on `causer` traversal) so the ABCI mapping is explicit
// at the point of definition, and stdlib's multi-error Unwrap so errors.Is
// reaches both members.
type invalidSolutionError struct{ rule error }

func (e invalidSolutionError) Error() string {
	return fmt.Sprintf("%s: %s", ErrInvalidSolution.Error(), e.rule.Error())
}

// ABCICode is read by cosmossdk.io/errors.ABCIInfo, which baseapp calls to build
// the tx result (sdkerrors.ResponseExecTxResultWithEvents).
func (e invalidSolutionError) ABCICode() uint32 { return ErrInvalidSolution.ABCICode() }

// Codespace pairs with ABCICode: (codespace, code) is the client's dispatch key.
func (e invalidSolutionError) Codespace() string { return ErrInvalidSolution.Codespace() }

// Unwrap exposes both members to errors.Is/errors.As.
func (e invalidSolutionError) Unwrap() []error { return []error{ErrInvalidSolution, e.rule} }
