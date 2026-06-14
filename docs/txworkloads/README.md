# Transaction Workload Artifacts

This directory contains checked-in examples of value-free mmap transaction
workload reports. They are evidence artifacts, not throughput claims and not a
production correctness proof.

Each `*.json` file should be produced with disposable database and trace paths:

```bash
tmpdir=$(mktemp -d)
go run ./cmd/mmaptxworkload --transactions 8 --delete-every 2 --readers 2 --label reader-pinned-local --trace "$tmpdir/tx.jsonl" --redact-path "$tmpdir/tx.db" > docs/txworkloads/reader-pinned-local.json
go run ./cmd/mmaptxworkload --transactions 8 --delete-every 2 --reader-processes 1 --label process-reader-local --trace "$tmpdir/process-tx.jsonl" --redact-path "$tmpdir/process-tx.db" > docs/txworkloads/process-reader-local.json
go run ./cmd/mmaptxsummary docs/txworkloads/*.json > docs/txworkloads/summary.md
./scripts/verify-tx-workload-artifacts.sh
```

The JSON records transaction counts, optimistic conflicts, delete/reopen checks,
reader-pinned retired pages, and final reclaim state. The database and trace
paths are intentionally omitted when `--redact-path` is used. The
`path_redacted: true` and `trace_path_redacted: true` fields record those
omissions. `summary.md` is generated from the JSON and verified by
`scripts/verify-tx-workload-artifacts.sh`.

Current checked-in samples:

- `reader-pinned-local.json`: one local run with two read-only handles pinned
  across the write workload. It shows retired pages blocked while readers are
  active and then reclaimed into reusable free pages after those readers close.
- `process-reader-local.json`: one local run with a separate child process
  holding `OpenMmapReadOnly` while the writer runs the same transaction
  workload. This exercises the reader sidecar through process-owned slots, not
  only same-process handles.
