package types

import (
	"time"

	errorsmod "cosmossdk.io/errors"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
)

// ExtensionTypeURL is the Any type URL flagging a tx as unordered.
var ExtensionTypeURL = "/" + ProtoMessageName()

// ProtoMessageName returns the proto full name of UnorderedTxExtension.
func ProtoMessageName() string { return "conclave.unordered.v1.UnorderedTxExtension" }

// RegisterInterfaces registers the extension as a TxBody extension option
// so tx decoding resolves its Any.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*txtypes.TxExtensionOptionI)(nil), &UnorderedTxExtension{})
}

// ExtensionOptionChecker accepts exactly the unordered extension — wired
// into the standard RejectExtensionOptionsDecorator, which otherwise
// rejects all TxBody extension options (D-026).
func ExtensionOptionChecker(any *codectypes.Any) bool {
	return any != nil && any.TypeUrl == ExtensionTypeURL
}

// TimeoutFromTx returns the unordered timeout_timestamp if the tx carries
// the extension. A tx is unordered iff found is true. A present-but-
// malformed extension returns an error: it must never be silently treated
// as ordered (the dedup entry would not be written, re-enabling replay).
func TimeoutFromTx(tx sdk.Tx) (timeout time.Time, found bool, err error) {
	extTx, ok := tx.(ante.HasExtensionOptionsTx)
	if !ok {
		return time.Time{}, false, nil
	}
	for _, opt := range extTx.GetExtensionOptions() {
		if opt == nil || opt.TypeUrl != ExtensionTypeURL {
			continue
		}
		var ext UnorderedTxExtension
		if err := ext.Unmarshal(opt.Value); err != nil {
			return time.Time{}, false, errorsmod.Wrapf(ErrInvalidExt, "%s", err)
		}
		return ext.TimeoutTimestamp, true, nil
	}
	return time.Time{}, false, nil
}
