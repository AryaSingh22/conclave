#!/usr/bin/env bash
# Sync a read-only browsing mirror of the repo into the Windows project folder.
# Run after each completed SPEC §10 task. The WSL repo (~/conclave) is canonical.
set -euo pipefail
SRC="$HOME/conclave"
DST="/mnt/c/Pro/Conclave Protocol/conclave"

cd "$SRC"
if [ -f scripts/dev-localnet-debug.sh ]; then
  git rm -q scripts/dev-localnet-debug.sh
  git commit -q -m "chore: drop obsolete localnet debug helper

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
fi

mkdir -p "$DST"
if command -v rsync >/dev/null 2>&1; then
  rsync -a --delete --exclude .git "$SRC/" "$DST/"
else
  rm -rf "$DST"
  mkdir -p "$DST"
  # shellcheck disable=SC2046
  cp -r $(ls -A "$SRC" | grep -v '^\.git$' | sed "s|^|$SRC/|") "$DST/"
fi

cat > "$DST/MIRROR-README.txt" <<'EOF'
This folder is a READ-ONLY MIRROR of the canonical Conclave repo, synced
after each completed task for browsing from Windows.

Canonical repo: \\wsl.localhost\Ubuntu\home\arya-singh\conclave
                (open with VS Code "WSL: Open Folder in WSL" -> /home/arya-singh/conclave)

Do NOT edit files here - changes will be overwritten on the next sync.
EOF

echo "=== mirror synced ==="
ls "$DST" | head -20
echo "=== git log ==="
git -C "$SRC" log --oneline
