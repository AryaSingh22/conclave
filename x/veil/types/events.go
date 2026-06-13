package types

// x/veil event types and attribute keys (SPEC §6.3 lifecycle).
const (
	EventTypeIntentSubmitted = "intent_submitted"
	EventTypeIntentSealed    = "intent_sealed"
	EventTypeIntentRevealed  = "intent_revealed"
	EventTypeIntentInvalid   = "intent_invalid"
	EventTypeIntentCancelled = "intent_cancelled"
	EventTypeIntentExpired   = "intent_expired"

	AttributeKeyIntentID    = "intent_id"
	AttributeKeySenderSigil = "sender_sigil"
	AttributeKeyOwner       = "owner"
	AttributeKeySessionID   = "session_id"
	AttributeKeyBond        = "bond"
	AttributeKeySlashed     = "slashed"
	AttributeKeyRefunded    = "refunded"
	AttributeKeyTTLHeight   = "ttl_height"
)
