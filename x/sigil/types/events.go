package types

// x/sigil event types and attribute keys.
const (
	EventTypeRegisterSigil    = "register_sigil"
	EventTypeRotateController = "rotate_controller"
	EventTypeAnchorCredential = "anchor_credential"
	EventTypeRevokeCredential = "revoke_credential"
	EventTypeReputationUpdate = "reputation_update"

	AttributeKeySigilID      = "sigil_id"
	AttributeKeyController   = "controller"
	AttributeKeyCredentialID = "credential_id"
	AttributeKeyIssuerSigil  = "issuer_sigil"
	AttributeKeyScore        = "score"
	AttributeKeyChamberID    = "chamber_id"
)
