package types

// x/mandate event types and attribute keys (SPEC §6.2 "Events"). The spec
// names mandate_issued, mandate_enforced, and mandate_window_reset;
// mandate_revoked, mandate_amended, and mandate_fee_converted are additive
// operational events (D-022).
const (
	EventTypeMandateIssued      = "mandate_issued"
	EventTypeMandateRevoked     = "mandate_revoked"
	EventTypeMandateAmended     = "mandate_amended"
	EventTypeMandateEnforced    = "mandate_enforced"
	EventTypeMandateWindowReset = "mandate_window_reset"
	EventTypeFeeConverted       = "mandate_fee_converted"

	AttributeKeyMandateID   = "mandate_id"
	AttributeKeySigilID     = "sigil_id"
	AttributeKeyGrantee     = "grantee"
	AttributeKeyController  = "controller"
	AttributeKeyReason      = "reason"
	AttributeKeySpendIntent = "spend_intent"
	AttributeKeyWindowSpent = "window_spent"
	AttributeKeyWindowStart = "window_start_height"
	AttributeKeyFee         = "fee"
	AttributeKeyFeeClv      = "converted_fee_uclv"
	AttributeKeyFeePayer    = "fee_payer"
)
