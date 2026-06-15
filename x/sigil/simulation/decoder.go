package simulation

import (
	"bytes"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/kv"

	"github.com/AryaSingh22/conclave/x/sigil/types"
)

// NewDecodeStore returns a StoreDecoder that renders x/sigil key-value pairs for
// the simulation's store-comparison diff (used by the import/export sim check).
func NewDecodeStore(cdc codec.Codec) func(kvA, kvB kv.Pair) string {
	return func(kvA, kvB kv.Pair) string {
		switch {
		case bytes.HasPrefix(kvA.Key, types.SigilKeyPrefix):
			var a, b types.Sigil
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case bytes.HasPrefix(kvA.Key, types.CredentialKeyPrefix):
			var a, b types.CredentialAnchor
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case bytes.HasPrefix(kvA.Key, types.ReputationKeyPrefix):
			var a, b types.Reputation
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case bytes.HasPrefix(kvA.Key, types.AddrIndexKeyPrefix):
			return fmt.Sprintf("%s\n%s", kvA.Value, kvB.Value)
		default:
			return fmt.Sprintf("%X\n%X", kvA.Value, kvB.Value)
		}
	}
}
