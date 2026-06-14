#!/usr/bin/env sh
set -eu

artifact_dir="docs/txworkloads"
summary="$artifact_dir/summary.md"

if [ ! -d "$artifact_dir" ]; then
  echo "missing $artifact_dir" >&2
  exit 1
fi

json_files=$(find "$artifact_dir" -name '*.json' -type f | sort)
if [ -z "$json_files" ]; then
  echo "missing transaction workload JSON artifacts in $artifact_dir" >&2
  exit 1
fi

for json_file in $json_files; do
  grep -F '"path_redacted": true' "$json_file" >/dev/null || {
    echo "$json_file is not path-redacted" >&2
    exit 1
  }
  grep -F '"trace_path_redacted": true' "$json_file" >/dev/null || {
    echo "$json_file is not trace-path-redacted" >&2
    exit 1
  }
  grep -F '"label":' "$json_file" >/dev/null || {
    echo "$json_file is missing a stable label" >&2
    exit 1
  }
  if grep -E '"path"[[:space:]]*:' "$json_file" >/dev/null; then
    echo "$json_file still exposes path" >&2
    exit 1
  fi
  if grep -E '"trace_path"[[:space:]]*:' "$json_file" >/dev/null; then
    echo "$json_file still exposes trace path" >&2
    exit 1
  fi
done

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

go run ./cmd/mmaptxsummary $json_files > "$tmp"
diff -u "$summary" "$tmp"
