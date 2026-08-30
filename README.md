# Conclave 
 
Conclave is a Layer-1 blockchain whose transaction layer is a consensus-native
sealed-bid exchange for autonomous agents — agents submit Sealed Intents under
Mandates, Sessions convene behind the Veil, and the conclave Concludes
atomically.

**The single source of truth for this repository is [docs/SPEC.md](docs/SPEC.md)
(Conclave Architecture & Build Specification v1.0).** If code and spec
conflict, the spec wins.

## Stack

- Go ≥ 1.22, Cosmos SDK v0.50.x (depinject wiring), CometBFT v0.38.x, ibc-go v8
- Chain ID (dev): `conclave-dev-1` · bech32 prefix: `conc` · base denom: `uclv` (10^6 uclv = 1 CLV)

## Quickstart (WSL2 Ubuntu / Linux)

```bash
make install          # builds conclaved into $GOPATH/bin
./scripts/localnet.sh # init + start single-node devnet with funded test accounts
```

Development workflow:

```bash
make lint test        # required green before any task is considered done (SPEC §13)
```

## Repository layout

See SPEC §4. Modules land in SPEC §10 task order: `x/sigil` → `x/mandate` →
`x/veil` → `x/session` → `x/escrow` (protos only in Phase 0) → `x/receipt`.

All stubbed functionality is marked `// CONCLAVE-TODO(phase-N)` and is
greppable.
