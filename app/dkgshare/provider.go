// Package dkgshare is the node-local DKG secret-share store (Task 1.7, D-070).
// The off-chain DKG daemon WRITES a node's finalized epoch share here; the app's
// vote-extension ExtendVote READS it. The secret share is node-local and NEVER
// enters consensus state or the p2p network (D-061/D-062): it lives only in this
// node's data directory (0600) and an in-memory cache. This leaf package is
// imported by both app (to build its ShareProvider) and app/dkgdaemon (to write),
// so the file format is shared without app↔daemon coupling.
package dkgshare

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/AryaSingh22/conclave/crypto/reveal"
)

// DirName is the per-node subdirectory (under the node home's data dir) holding
// epoch share files.
const DirName = "dkg_shares"

// FileProvider is a file-backed, node-local ShareProvider. It satisfies the
// app.ShareProvider interface (RevealMaterial). Reads are served from an
// in-memory cache backed by per-epoch files, so a node restart mid-epoch
// re-loads its shares and keeps revealing.
type FileProvider struct {
	dir string

	mu    sync.RWMutex
	cache map[uint64]material
}

type material struct {
	idx   uint32
	share reveal.KeyShare
}

// NewFileProvider builds a file-backed provider rooted at dir (created if absent,
// 0700). dir is typically <home>/data/dkg_shares.
func NewFileProvider(dir string) (*FileProvider, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("dkgshare: mkdir %s: %w", dir, err)
	}
	return &FileProvider{dir: dir, cache: map[uint64]material{}}, nil
}

func (p *FileProvider) path(epochID uint64) string {
	return filepath.Join(p.dir, fmt.Sprintf("epoch_%d.share", epochID))
}

// RevealMaterial implements app.ShareProvider: this node's (dkg_index, secret
// share) for an epoch, or ok=false if it holds none (not in committee / DKG
// incomplete). Cache-then-disk so a restart re-loads.
func (p *FileProvider) RevealMaterial(epochID uint64) (uint32, reveal.KeyShare, bool) {
	p.mu.RLock()
	m, ok := p.cache[epochID]
	p.mu.RUnlock()
	if ok {
		return m.idx, m.share, true
	}
	// disk fallback (post-restart).
	idx, share, err := readShareFile(p.path(epochID))
	if err != nil {
		return 0, nil, false
	}
	p.mu.Lock()
	p.cache[epochID] = material{idx: idx, share: share}
	p.mu.Unlock()
	return idx, share, true
}

// Store persists this node's finalized share for an epoch (0600) and caches it.
// Called by the DKG daemon at the close of a successful DKG.
func (p *FileProvider) Store(epochID uint64, dkgIndex uint32, share reveal.KeyShare) error {
	if err := writeShareFile(p.path(epochID), dkgIndex, share); err != nil {
		return err
	}
	p.mu.Lock()
	p.cache[epochID] = material{idx: dkgIndex, share: append(reveal.KeyShare{}, share...)}
	p.mu.Unlock()
	return nil
}

// Has reports whether a share file already exists for an epoch (daemon
// idempotency — don't re-run a completed DKG after restart).
func (p *FileProvider) Has(epochID uint64) bool {
	if _, _, err := readShareFile(p.path(epochID)); err == nil {
		return true
	}
	return false
}

// --- file format: dkg_index(4 BE) | share bytes ---

func writeShareFile(path string, idx uint32, share reveal.KeyShare) error {
	buf := make([]byte, 4+len(share))
	binary.BigEndian.PutUint32(buf[:4], idx)
	copy(buf[4:], share)
	// write to a temp file then rename for atomicity.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("dkgshare: write %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

func readShareFile(path string) (uint32, reveal.KeyShare, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, nil, err
	}
	if len(b) < 4 {
		return 0, nil, errors.New("dkgshare: truncated share file")
	}
	idx := binary.BigEndian.Uint32(b[:4])
	share := append(reveal.KeyShare{}, b[4:]...)
	return idx, share, nil
}
