package types

// x/epoch event types + attribute keys (observability for the epoch lifecycle).
const (
	EventTypeEpochForming = "epoch_forming"
	EventTypeEpochActive  = "epoch_active"
	EventTypeEpochFailed  = "epoch_failed"
	EventTypeEpochRotated = "epoch_rotated"
	EventTypeEpochRetired = "epoch_retired"

	AttributeKeyEpochID       = "epoch_id"
	AttributeKeyStatus        = "status"
	AttributeKeyCommitteeSize = "committee_size"
	AttributeKeyThreshold     = "threshold"
	AttributeKeyConfirmations = "confirmations"
	AttributeKeyPrevEpochID   = "prev_epoch_id"
	AttributeKeyFailureReason = "failure_reason"
)
