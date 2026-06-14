# Filesystem Probe Artifacts

This directory contains checked-in examples of value-free mmap filesystem probe
runs. They are evidence artifacts, not benchmark claims and not a platform
support guarantee.

Each `*.json` file should be produced with a disposable database path:

```bash
go run ./cmd/mmapfsprobe --keys 256 --value-bytes 512 --label darwin-apfs-local --redact-path /path/to/probe.db > docs/probes/darwin-apfs-local.json
go run ./cmd/fsprobesummary docs/probes/*.json > docs/probes/summary.md
./scripts/verify-probe-artifacts.sh
```

The JSON keeps filesystem and mount identity because those are part of the
experiment. The database path is intentionally omitted when `--redact-path` is
used, and `path_redacted: true` records that omission. `summary.md` is generated
from the JSON and verified by `scripts/verify-probe-artifacts.sh`.

Current checked-in samples:

- `darwin-apfs-local.json`: one local Darwin/APFS run. It shows APFS did not
  allocate sparse holes for this workload and that sparse punching is not wired
  for Darwin in this lab build.
