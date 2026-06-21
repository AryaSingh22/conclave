package dkgshare

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LongTermKeyFile is the node-local DKG long-term SECRET key file name (under the
// node home's config dir). It holds the marshaled BLS12-381 G2 long-term secret
// the DKG daemon deals with; the matching public is what the node registers
// on-chain (Task 1.7 A2, D-070). Like priv_validator_key.json it is node-private
// (0600) and never leaves the node.
const LongTermKeyFile = "dkg_longterm_key"

// LongTermKeyPath returns <home>/config/dkg_longterm_key.
func LongTermKeyPath(home string) string {
	return filepath.Join(home, "config", LongTermKeyFile)
}

// WriteLongTermSecret persists the marshaled long-term DKG secret (0600). It
// refuses to overwrite an existing key (regenerating would orphan every epoch
// share derived from the old key).
func WriteLongTermSecret(home string, secret []byte) error {
	path := LongTermKeyPath(home)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("dkgshare: long-term key already exists at %s (refusing to overwrite)", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, secret, 0o600)
}

// ReadLongTermSecret loads the marshaled long-term DKG secret from the node home.
func ReadLongTermSecret(home string) ([]byte, error) {
	b, err := os.ReadFile(LongTermKeyPath(home))
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, errors.New("dkgshare: empty long-term key file")
	}
	return b, nil
}
