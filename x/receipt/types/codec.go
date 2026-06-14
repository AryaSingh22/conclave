package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
)

// RegisterLegacyAminoCodec is a no-op: x/receipt is a query/proof surface with
// no Msgs (SPEC §6.6).
func RegisterLegacyAminoCodec(_ *codec.LegacyAmino) {}

// RegisterInterfaces is a no-op: x/receipt registers no sdk.Msg implementations
// (SPEC §6.6 — query/proof only, no consensus writes).
func RegisterInterfaces(_ codectypes.InterfaceRegistry) {}
