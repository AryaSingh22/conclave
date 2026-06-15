package simulation

import (
	"bytes"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/kv"

	receipttypes "github.com/AryaSingh22/conclave/x/receipt/types"
	"github.com/AryaSingh22/conclave/x/session/types"
)

// NewDecodeStore returns a StoreDecoder for x/session key-value pairs (sessions,
// solvers, solutions, receipts — D-045).
func NewDecodeStore(cdc codec.Codec) func(kvA, kvB kv.Pair) string {
	return func(kvA, kvB kv.Pair) string {
		switch {
		case bytes.HasPrefix(kvA.Key, types.SessionKeyPrefix):
			var a, b types.Session
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case bytes.HasPrefix(kvA.Key, types.SolverKeyPrefix):
			var a, b types.Solver
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case bytes.HasPrefix(kvA.Key, types.SolutionKeyPrefix):
			var a, b types.Solution
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case bytes.HasPrefix(kvA.Key, types.ReceiptKeyPrefix):
			var a, b receipttypes.Receipt
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		default:
			return fmt.Sprintf("%X\n%X", kvA.Value, kvB.Value)
		}
	}
}
