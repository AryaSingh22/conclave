package types

// x/session event types and attribute keys (SPEC §6.4 lifecycle).
const (
	EventTypeSolverRegistered = "solver_registered"
	EventTypeSolverUnbonding  = "solver_unbonding"
	EventTypeSolverUnbonded   = "solver_unbonded"
	EventTypeSolverSlashed    = "solver_slashed"
	EventTypeSolutionProposed = "solution_proposed"
	EventTypeSessionConcluded = "session_concluded"
	EventTypeSessionFallback  = "session_fallback"
	EventTypeMatchSettled     = "match_settled"
	EventTypeEscrowOpened     = "escrow_opened"
	EventTypeReceiptEmitted   = "receipt_emitted"
	EventTypeSkimTaken        = "skim_taken"
	EventTypeSolverPaid       = "solver_paid"
	EventTypeIntentRolled     = "intent_rolled"

	AttributeKeySolver       = "solver"
	AttributeKeyBond         = "bond"
	AttributeKeySessionID    = "session_id"
	AttributeKeySolutionID   = "solution_id"
	AttributeKeySlashed      = "slashed"
	AttributeKeyReason       = "reason"
	AttributeKeyUnbondHeight = "unbond_height"
	AttributeKeyEscrowID     = "escrow_id"
	AttributeKeyIntentID     = "intent_id"
	AttributeKeyReceiptID    = "receipt_id"
	AttributeKeySigilID      = "sigil_id"
	AttributeKeyAmount       = "amount"
	AttributeKeyScore        = "score"
)
