package simulation

import (
	"bytes"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/kv"

	"github.com/AryaSingh22/conclave/x/veil/types"
)

// NewDecodeStore returns a StoreDecoder for x/veil key-value pairs.
func NewDecodeStore(cdc codec.Codec) func(kvA, kvB kv.Pair) string {
	return func(kvA, kvB kv.Pair) string {
		switch {
		case bytes.HasPrefix(kvA.Key, types.IntentKeyPrefix):
			var a, b types.IntentRecord
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case bytes.HasPrefix(kvA.Key, types.StateIndexKeyPrefix):
			return fmt.Sprintf("%X\n%X", kvA.Value, kvB.Value)
		default:
			return fmt.Sprintf("%X\n%X", kvA.Value, kvB.Value)
		}
	}
}
