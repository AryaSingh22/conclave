package types

// x/escrow event types and attribute keys (SPEC §6.5).
const (
	// EventTypeEscrowOpened is emitted when an escrow record is created. The
	// string is byte-identical to the pre-handoff x/session event so existing
	// observers (and the conclude tests) keep matching across the D-073
	// ownership handoff.
	EventTypeEscrowOpened   = "escrow_opened"
	EventTypeEscrowReleased = "escrow_released"
	EventTypeEscrowRefunded = "escrow_refunded"
	EventTypeEscrowDisputed = "escrow_disputed"

	AttributeKeyEscrowID  = "escrow_id"
	AttributeKeySessionID = "session_id"
	AttributeKeyState     = "state"
	AttributeKeyReason    = "reason"
)
