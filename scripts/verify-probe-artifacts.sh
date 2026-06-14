#!/usr/bin/env sh
set -eu

probe_dir="docs/probes"
summary="$probe_dir/summary.md"

if [ ! -d "$probe_dir" ]; then
  echo "missing $probe_dir" >&2
  exit 1
fi

json_files=$(find "$probe_dir" -name '*.json' -type f | sort)
if [ -z "$json_files" ]; then
  echo "missing probe JSON artifacts in $probe_dir" >&2
  exit 1
fi

for json_file in $json_files; do
  grep -F '"path_redacted": true' "$json_file" >/dev/null || {
    echo "$json_file is not path-redacted" >&2
    exit 1
  }
  if grep -E '"path"[[:space:]]*:' "$json_file" >/dev/null; then
    echo "$json_file still exposes path" >&2
    exit 1
  fi
done

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

go run ./cmd/fsprobesummary $json_files > "$tmp"
diff -u "$summary" "$tmp"
