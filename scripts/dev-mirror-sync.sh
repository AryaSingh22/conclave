#!/usr/bin/env bash
# Sync a read-only browsing mirror of the repo into the Windows project folder.
# Run after each completed SPEC §10 task. The WSL repo (~/conclave) is canonical.
set -euo pipefail
SRC="$HOME/conclave"
DST="/mnt/c/Pro/Conclave Protocol/conclave"

cd "$SRC"

mkdir -p "$DST"
if command -v rsync >/dev/null 2>&1; then
  rsync -a --delete --exclude .git "$SRC/" "$DST/"
else
  rm -rf "$DST"
  mkdir -p "$DST"
  # shellcheck disable=SC2046
  cp -r $(ls -A "$SRC" | grep -v '^\.git$' | sed "s|^|$SRC/|") "$DST/"
fi

# --- Remove any .git in the mirror (D-090) ---------------------------------
# The rsync above carries --exclude .git, which means it never WRITES a .git
# into the mirror -- but it also never UPDATES or REMOVES one already there.
# A .git left behind by an earlier full copy therefore freezes at whatever
# commit it was made from while the working files keep advancing, and every
# git command run in the mirror then reports confidently wrong state:
# `git log` shows a stale HEAD, `git status` shows dozens of phantom
# "modified" files (the synced-forward content diffed against the frozen
# index). This was live -- the mirror sat at b2f633b, five commits behind,
# with 40+ phantom modifications, until the D-090 reconciliation audit.
#
# The hazard is not the misreport, it is the recovery reflex: `git checkout .`
# or `git stash` in that folder would "discard" the phantom changes and
# silently revert the mirror to the frozen commit's content. Content
# visibility is the mirror's only job, so a half-stale .git is pure downside.
# Deleted unconditionally, every sync.
if [ -d "$DST/.git" ] || [ -f "$DST/.git" ]; then
  echo "=== removing stale .git from mirror (D-090) ==="
  rm -rf "$DST/.git"
fi

cat > "$DST/MIRROR-README.txt" <<'EOF'
This folder is a READ-ONLY MIRROR of the canonical Conclave repo, synced
after each completed task for browsing from Windows.

Canonical repo: \\wsl.localhost\Ubuntu\home\arya-singh\conclave
                (open with VS Code "WSL: Open Folder in WSL" -> /home/arya-singh/conclave)

Do NOT edit files here - changes will be overwritten on the next sync.

THERE IS DELIBERATELY NO .git HERE. This is not a clone and not a worktree:
it is an rsync of tracked+untracked files with no history. Any git command
run in this folder is either an error or, worse, answering about some other
repository. Do not run git here - and never `git checkout`/`git stash`/
`git push` here. Use the canonical WSL repo above for all git operations.
EOF

# A sentinel at the root, so the hazard is visible in a plain directory
# listing and not only to someone who opens the README.
cat > "$DST/MIRROR-READ-ONLY-DO-NOT-PUSH.txt" <<'EOF'
READ-ONLY MIRROR - NOT A GIT REPOSITORY - DO NOT COMMIT OR PUSH FROM HERE.
Canonical repo: \\wsl.localhost\Ubuntu\home\arya-singh\conclave
Edits made here are destroyed by the next `scripts/dev-mirror-sync.sh` run.
EOF

echo "=== mirror synced ==="
ls "$DST" | head -20
echo "=== mirror .git present? (expect: no) ==="
if [ -e "$DST/.git" ]; then echo "WARNING: .git STILL PRESENT in mirror"; else echo "none - correct"; fi
echo "=== canonical HEAD (the commit this mirror reflects) ==="
git -C "$SRC" log --oneline -1
