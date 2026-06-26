// Command gen runs the ZK_DELIVERY trusted setup and writes the artifacts the
// rest of the package consumes:
//
//	crypto/zkdelivery/vk/delivery_v1.vk           — the verifying key (embedded
//	                                                 on-chain by the verifier)
//	crypto/zkdelivery/zktest/testdata/delivery_v1.pk — the proving key (test-only,
//	                                                 embedded by zktest; NEVER in
//	                                                 the consensus binary)
//
// Run: `go run ./crypto/zkdelivery/gen` (from the repo root). Re-run ONLY when
// DeliveryCircuit changes, and commit the regenerated artifacts.
//
// The setup randomness ("toxic waste") is OFF-CHAIN / build-time, NOT consensus
// — exactly like the DKG's off-chain crypto/rand (D-066). The on-chain path is
// only groth16.Verify (deterministic, randomness-free). This dev setup is a
// single-party generation; a production deployment replaces it with an MPC
// ceremony (a mainnet gate, the analog of D-066's external-audit gate).
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"

	"github.com/AryaSingh22/conclave/crypto/zkdelivery/circuit"
)

func main() {
	ccs, err := frontend.Compile(circuit.Curve.ScalarField(), r1cs.NewBuilder, &circuit.DeliveryCircuit{})
	must("compile", err)
	pk, vk, err := groth16.Setup(ccs)
	must("setup", err)

	base := packageDir()
	writeTo(filepath.Join(base, "vk", "delivery_v1.vk"), vk)
	writeTo(filepath.Join(base, "zktest", "testdata", "delivery_v1.pk"), pk)
	fmt.Printf("zkdelivery gen: wrote vk + pk (%d constraints) under %s\n", ccs.GetNbConstraints(), base)
}

// packageDir returns crypto/zkdelivery (this file lives in its gen/ subdir).
func packageDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("zkdelivery gen: cannot resolve caller path")
	}
	return filepath.Dir(filepath.Dir(thisFile))
}

func writeTo(path string, obj io.WriterTo) {
	must("mkdir "+path, os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.Create(path)
	must("create "+path, err)
	defer f.Close()
	_, err = obj.WriteTo(f)
	must("write "+path, err)
}

func must(stage string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "zkdelivery gen FAILED at %s: %v\n", stage, err)
		os.Exit(1)
	}
}
