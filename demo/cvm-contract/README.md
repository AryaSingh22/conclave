# conclave-demo-escrow — the CVM-schema demo contract

The purpose-built CosmWasm contract the **Phase-2 §6 exit demo** uses to drive
an escrow release from inside a real wasm VM on a real network. It exists to
discharge the one obligation task 2.2 deferred (**D-079**): wasmd's `reflect`
contract has a *typed* Custom message (`debug|raw`) and therefore cannot spell
Conclave's CVM schema at all, so the live-localnet contract-release slice had no
vehicle until this crate existed.

## What it is, and what it is not

- **Dev tooling.** Not a Go dependency, absent from `go.mod`, not built by
  `make test`, and **nothing it produces enters `conclaved`**.
- **A transparent conduit.** It holds no state and makes no decisions: each
  `ExecuteMsg` emits exactly one CVM custom message. That is deliberate — it
  means anything the demo observes is attributable to the **bindings**
  (`app/cvm/encoder.go`) and the **escrow keeper**, never to contract logic.
- **Its schema is a contract.** Variant and field names must match
  `app/cvm/encoder.go` exactly: that encoder decodes with
  `DisallowUnknownFields` and requires exactly one branch set. The in-crate test
  `submit_delivery_proof_serializes_to_the_cvm_schema` pins the exact JSON, so a
  rename on either side fails at `cargo test` rather than at demo time.

The `conclude` branch of `ConclaveMsg` is **deliberately not allowlisted** and
**deliberately present**: the demo needs a real contract that genuinely attempts
settlement authorship, so layer 1 of the D-079 authority model is watched
rejecting a spelling a live contract actually emitted.

## Toolchain

| Pin | Value | Why |
|---|---|---|
| `cosmwasm-std` / `cosmwasm-schema` | `=2.2.8` (exact) | the node's `wasmvm` is **v2.2.8** (D-079). A caret range floats to 2.3.x — a different VM line than the one pinned in `go.mod`. Exact-pinning keeps the contract stdlib and the VM that runs it on the same release (METHODOLOGY Rule 5 applies to dev tooling too) |
| Rust target | `wasm32-unknown-unknown` | `rustup target add wasm32-unknown-unknown` |
| Rust | 1.85.0 (built with); crate floor 1.78 | |

## Build

```sh
make demo-contract          # from the repo root
# or, equivalently:
cd demo/cvm-contract && cargo build --release --target wasm32-unknown-unknown
cp target/wasm32-unknown-unknown/release/conclave_demo_escrow.wasm artifacts/
```

**The built artifact is committed** at
`artifacts/conclave_demo_escrow.wasm` (~212 KB), so the demo runs on a machine
with no Rust toolchain. This follows the precedent already set by the committed
groth16 verifying/proving keys (`crypto/zkdelivery/vk`, `.../zktest/testdata`).
`target/` is git-ignored.

## Upload posture

The default genesis ships `CodeUploadAccess=Nobody` and
`InstantiateDefaultPermission=Nobody` (D-079). The demo therefore uploads this
contract **through a governance proposal**, with the proposal's
`instantiate_permission` naming the demo key — exercising the permissioned
posture live rather than loosening genesis to avoid it.
