package app

import "encoding/json"

// GenesisState of the blockchain is represented here as a map of raw json
// messages keyed by an identifier string: one entry per module, where the
// identifier is the module name and the value is the module's raw genesis.
type GenesisState map[string]json.RawMessage
