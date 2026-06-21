package types

// x/veil event types and attribute keys (SPEC §6.3 lifecycle).
const (
	EventTypeIntentSubmitted = "intent_submitted"
	EventTypeIntentSealed    = "intent_sealed"
	EventTypeIntentRevealed  = "intent_revealed"
	EventTypeIntentInvalid   = "intent_invalid"
	EventTypeIntentCancelled = "intent_cancelled"
	EventTypeIntentExpired   = "intent_expired"
	EventTypeIntentConcluded = "intent_concluded"
	EventTypeIntentRolled    = "intent_rolled"
	// EventTypeRevealRetry: a session fell short of t valid reveal shares and
	// will retry next block (Task 1.6, PHASE1_DESIGN §2(c)).
	EventTypeRevealRetry = "reveal_retry"
	// EventTypeSessionCancelled: a session exhausted max_reveal_retries; its
	// still-sealed intents CANCEL (bonds refunded), releasing epoch rotation.
	EventTypeSessionCancelled = "session_reveal_cancelled"

	AttributeKeyIntentID    = "intent_id"
	AttributeKeySenderSigil = "sender_sigil"
	AttributeKeyOwner       = "owner"
	AttributeKeySessionID   = "session_id"
	AttributeKeyBond        = "bond"
	AttributeKeySlashed     = "slashed"
	AttributeKeyRefunded    = "refunded"
	AttributeKeyTTLHeight   = "ttl_height"
	// AttributeKeyAttempts is the reveal-attempt count on a reveal_retry /
	// session_reveal_cancelled event (Task 1.6).
	AttributeKeyAttempts = "attempts"
	// AttributeKeyReason distinguishes the two §6.3 INVALID causes on the
	// intent_invalid event (D-065): a malformed payload vs a spend_commitment
	// violation. Both slash 50% identically; the reason is for observability.
	AttributeKeyReason = "reason"

	// InvalidReasonMalformedPayload: the payload failed proto-unmarshal at the
	// Reveal (D-040).
	InvalidReasonMalformedPayload = "malformed_payload"
	// InvalidReasonCommitmentViolation: the revealed give legs exceed the
	// declared spend_commitment in some denom (under-declaration, D-065).
	InvalidReasonCommitmentViolation = "commitment_violation"
)
