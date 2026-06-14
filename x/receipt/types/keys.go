package types

// ModuleName is the SPEC §0 naming-canon name for the receipt query/proof
// surface (SPEC §6.6). x/receipt is a READ-ONLY surface over the receipts
// x/session writes (D-045): it owns NO store and is registered WITHOUT a
// KVStoreKey, so it is structurally incapable of a consensus state write
// (D-052). There is deliberately no StoreKey constant here.
const ModuleName = "receipt"
