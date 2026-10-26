#!/usr/bin/env bash
# Fails when an exported identifier of the varmq package is never mentioned in
# the docs. An identifier counts as covered when it appears in backticks,
# `Name`, `Name(`, `Name[T]` or qualified like `worker.Name()`, or in a
# pkg.go.dev anchor link, #Name or #Type.Method. A bare word does not count,
# and neither does code inside a fenced block.
#
# The API is read from the varmq release docs/examples/go.mod pins, the one the
# docs describe, not from the source at the repo root.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
cd "$(git rev-parse --show-toplevel)"

pkg=github.com/goptics/varmq
docs=docs/src/content/docs

# One "name<TAB>label" line per exported identifier, read from `go doc -all`:
# funcs, types, single const/var lines, every name in a const or var group
# (go doc -short collapses those), and the methods of each interface.
# go doc does not fetch modules, so download the pinned one first.
wanted() {
  (cd docs/examples && go mod download "$pkg" && go doc -all "$pkg") | awk -F'\t' '
    /^func [A-Z]/      { split($0, a, /[ (\[]/); print a[2] "\t" a[2]; next }
    /^type [A-Z]/      { split($0, a, /[ \[]/);  print a[2] "\t" a[2]
                         iface = ($0 ~ / interface \{$/) ? a[2] : ""; next }
    /^(const|var) [A-Z]/ { split($0, a, /[ ,]/); print a[2] "\t" a[2]; next }
    /^(const|var) \($/ { group = 1; next }
    /^\)$/             { group = 0; next }
    /^}$/              { iface = ""; next }
    group && /^\t[A-Z]/ { split($0, a, /[ \t]/); print a[2] "\t" a[2]; next }
    iface != "" && /^\t[A-Z][A-Za-z0-9_]*\(/ {
      split($0, a, /[\t(]/); print a[2] "\t" iface "." a[2]
    }'
}

# Every name the docs mention, one per line.
covered=$(grep -rhoE \
  -e '`([A-Za-z0-9_]+\.)*[A-Za-z_][A-Za-z0-9_]*[`(\[]' \
  -e "pkg\.go\.dev/${pkg}#([A-Za-z0-9_]+\.)?[A-Za-z_][A-Za-z0-9_]*" \
  "$docs" |
  sed -E 's/[`(\[]$//; s/^.*[`.#]//' | sort -u)

# Read into a variable first, so a failing `go doc` stops the script instead of
# leaving an empty list that would pass.
names=$(wanted | sort -u -t$'\t' -k1,1)
if [[ -z $names ]]; then
  echo "API coverage: go doc listed no identifiers for $pkg." >&2
  exit 1
fi

missing=()
while IFS=$'\t' read -r name label; do
  if ! grep -qxF "$name" <<<"$covered"; then
    missing+=("$label")
  fi
done <<<"$names"

if ((${#missing[@]} > 0)); then
  echo "API coverage: ${#missing[@]} exported identifiers of $pkg are not documented:" >&2
  printf '  %s\n' "${missing[@]}" >&2
  exit 1
fi
echo "API coverage: every exported identifier of $pkg is documented."
