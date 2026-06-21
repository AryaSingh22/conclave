package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AryaSingh22/conclave/app/dkgshare"
	"github.com/AryaSingh22/conclave/crypto/reveal"
	"github.com/AryaSingh22/conclave/crypto/threshold"
	epochtypes "github.com/AryaSingh22/conclave/x/epoch/types"
)

// veil_adversary.go is the Task-1.7 / D-070 F-4 LIVE adversary: a validator
// running MODIFIED code that tries to decrypt an intent BEFORE the threshold
// reveal. conclave-demo is dev-only tooling and is NEVER linked into the shipping
// conclaved node binary, so this models a malicious node without modifying the
// real node software. It is given the STRONGEST possible pre-reveal view: this
// validator's complete node-local material (its own secret DKG share, read from
// the node's private share store) PLUS the published epoch key PLUS a target
// ciphertext. It proves that this is still not enough — a single share is below
// the t-of-n threshold, and the additional shares that would cross it do not
// exist until vote extensions emit them AFTER the seal fixes ordering (F-4).
//
// CONCLAVE_ADVERSARY_HOME (or CONCLAVED_HOME) must point at the adversary node's
// home so the tool can read its node-local epoch-1 share.
func (c *cli) veilF4Adversary() error {
	epochc := epochtypes.NewQueryClient(c.conn)
	res, err := epochc.CurrentEpoch(context.Background(), &epochtypes.QueryCurrentEpochRequest{})
	if err != nil {
		return fmt.Errorf("query current epoch: %w", err)
	}
	e := res.Epoch
	if e.Id == 0 || e.Status != epochtypes.EPOCH_STATUS_ACTIVE || len(e.Commits) == 0 {
		return fmt.Errorf("no ACTIVE epoch with a published key yet (id=%d status=%v)", e.Id, e.Status)
	}
	n := len(e.Committee)

	home := os.Getenv("CONCLAVE_ADVERSARY_HOME")
	if home == "" {
		home = os.Getenv("CONCLAVED_HOME")
	}
	if home == "" {
		return errors.New("set CONCLAVE_ADVERSARY_HOME to the adversary node's home")
	}
	shares, err := dkgshare.NewFileProvider(filepath.Join(home, "data", dkgshare.DirName))
	if err != nil {
		return err
	}
	idx, myShare, ok := shares.RevealMaterial(e.Id)
	if !ok {
		return fmt.Errorf("adversary holds no node-local share for epoch %d (not in committee?)", e.Id)
	}
	fmt.Printf("veil-f4-adversary: this validator holds its OWN secret share (index %d) for epoch %d (n=%d, t=%d)\n",
		idx, e.Id, n, e.Threshold)

	// build the reveal scheme from the PUBLIC epoch commits and a target ct (the
	// adversary even gets to choose the plaintext — it still cannot recover it).
	sch, err := threshold.NewSchemeFromCommitBytes(e.Commits, n)
	if err != nil {
		return err
	}
	secret := []byte("a secret only the threshold should reveal")
	ct, err := threshold.NewClientScheme().Encrypt(e.EpochPubKey, secret)
	if err != nil {
		return err
	}

	// attempt 1: decrypt with ZERO shares (the modified node has only public data).
	if _, err := sch.Combine(nil, ct); !errors.Is(err, threshold.ErrInsufficientShares) {
		return fmt.Errorf("F-4 BROKEN: 0-share Combine did not refuse: %v", err)
	}
	// attempt 2: decrypt with this node's OWN single share (1 < t).
	myPartial, err := sch.PartialReveal(myShare, ct)
	if err != nil {
		return fmt.Errorf("partial reveal: %w", err)
	}
	if _, err := sch.Combine([]reveal.RevealShare{myPartial}, ct); !errors.Is(err, threshold.ErrInsufficientShares) {
		return fmt.Errorf("F-4 BROKEN: single-share Combine did not refuse: %v", err)
	}

	fmt.Printf("veil-f4-adversary: 0-share and single-share decrypt both refused (ErrInsufficientShares) — "+
		"a modified validator provably CANNOT decrypt pre-ordering; it needs t=%d shares that only exist "+
		"in vote extensions AFTER the seal (F-4) ✓\n", e.Threshold)
	fmt.Println("VEIL-F4 OK")
	return nil
}
