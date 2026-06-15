package simulation

import (
	"bytes"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/kv"

	"github.com/AryaSingh22/conclave/x/mandate/types"
)

// NewDecodeStore returns a StoreDecoder for x/mandate key-value pairs.
func NewDecodeStore(cdc codec.Codec) func(kvA, kvB kv.Pair) string {
	return func(kvA, kvB kv.Pair) string {
		switch {
		case bytes.HasPrefix(kvA.Key, types.MandateKeyPrefix):
			var a, b types.Mandate
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case bytes.HasPrefix(kvA.Key, types.GranteeIndexKeyPrefix),
			bytes.HasPrefix(kvA.Key, types.SigilIndexKeyPrefix):
			return fmt.Sprintf("%X\n%X", kvA.Value, kvB.Value)
		default:
			return fmt.Sprintf("%X\n%X", kvA.Value, kvB.Value)
		}
	}
}
