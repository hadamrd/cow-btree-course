# Benchmarking And Baselines

This repository is a research storage-engine lab, so benchmark output is
evidence, not marketing. The goal is to make performance experiments
repeatable enough that a reader can see what changed and which workload was
measured.

The benchmark suite lives in [`pagebtree/bench_test.go`](../pagebtree/bench_test.go).
It covers point lookup, cursor seek/next, bounded range scans, forward and
reverse bounded cursors, sequential insert, sequential delete, mmap put+sync,
mmap delete+sync, and mmap reopen validation.

Run a short local smoke pass:

```bash
go test ./pagebtree -run '^$' -bench 'Benchmark(PageTree|MmapTree)' -benchmem -benchtime=100x
```

Capture a baseline artifact with repeated runs:

```bash
go test ./pagebtree -run '^$' -bench 'Benchmark(PageTree|MmapTree)' -benchmem -count=5 > bench.out
go run ./cmd/benchsummary bench.out > bench-summary.md
```

`bench.out` is the raw source of truth. `bench-summary.md` is a compact table
for review notes, commit messages, or manual baseline history:

```markdown
| Benchmark | Iterations | ns/op | B/op | allocs/op | keys/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| PageTreeGet | 1000 | 123.4 | 32 | 1 |  |
```

The summary command also accepts stdin:

```bash
go test ./pagebtree -run '^$' -bench 'BenchmarkMmapTreeGet' -benchmem \
  | go run ./cmd/benchsummary
```

## Reading Results

Compare benchmark output only when the machine, Go version, flags, and workload
are known. mmap benchmarks are especially sensitive to filesystem cache state,
writeback pressure, CPU frequency, background IO, and whether the benchmark file
is on local disk or a virtualized filesystem.

Use these numbers as a regression detector and a workload map:

- `ns/op` shows latency for the benchmarked operation.
- `B/op` and `allocs/op` show Go heap pressure; they do not include kernel page
  cache residency.
- `keys/op` appears on scan benchmarks to show how many keys each operation
  visited.

For serious comparison, keep the raw `bench.out` files next to any summary and
use an external statistical comparison tool such as `benchstat` over repeated
runs. The repository-local `benchsummary` command intentionally avoids claiming
statistical significance; it only makes the current Go benchmark rows easier to
read and archive.

## Current Limits

The suite is still small. It does not yet model multi-process reader pressure,
mixed read/write production traces, sparse-file behavior, online vacuum, or
long-running compaction workloads. Treat missing workload coverage as a research
gap, not proof that the engine is optimized.
