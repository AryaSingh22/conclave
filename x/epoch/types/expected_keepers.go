package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// StakingKeeper is the x/staking surface x/epoch depends on: the bonded
// validator set, snapshotted at each epoch boundary to form the DKG committee
// (PHASE1_DESIGN §2(b), D-068). x/epoch's EndBlocker is ordered AFTER staking's
// (app_config.go) so the set read here is the one staking has finalized for the
// block — exactly the validator set CometBFT will use, which is the
// deterministic resolution of §2(b)'s "valset-update trigger".
type StakingKeeper interface {
	GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error)
}

// VeilKeeper is the x/veil surface x/epoch depends on for drain-before-rotate
// (PHASE1_DESIGN §2(b), seam (a)/(c); D-068): whether an epoch still has
// in-flight (sealed-but-unrevealed) ciphertexts tagged to it. A DRAINING epoch
// retires only once this reports it fully drained. CANCEL/OPEN of those intents
// is what releases the rotation (the session-state half of the 1.4 crypto
// precondition).
type VeilKeeper interface {
	// HasInFlightForEpoch reports whether any intent that is sealed-but-not-yet-
	// revealed is tagged to epochID. Returns false for epochID==0 (stub/unset
	// intents are not bound to any DKG epoch).
	HasInFlightForEpoch(ctx sdk.Context, epochID uint64) bool
}
