# Platform Matrix

This chapter records the current platform contract for the mmap-backed tree. It
is deliberately narrower than a production support statement: the matrix says
which mechanics are compiled and locally checked, while filesystem-specific
crash ordering, sparse allocation behavior, and kernel version behavior remain
experimental evidence problems.

The executable source of truth is `pagebtree.MmapPlatformProfile()`:

- Public profile shape: [`../pagebtree/platform_profile.go`](../pagebtree/platform_profile.go)
- Unix mmap profile: [`../pagebtree/platform_profile_unix.go`](../pagebtree/platform_profile_unix.go)
- Linux extras: [`../pagebtree/platform_profile_linux.go`](../pagebtree/platform_profile_linux.go)
- Darwin extras: [`../pagebtree/platform_profile_darwin.go`](../pagebtree/platform_profile_darwin.go)
- Other Unix fallback: [`../pagebtree/platform_profile_unix_other.go`](../pagebtree/platform_profile_unix_other.go)
- Non-Unix stub: [`../pagebtree/platform_profile_unsupported.go`](../pagebtree/platform_profile_unsupported.go)
- JSON command: [`../cmd/mmapplatform/main.go`](../cmd/mmapplatform/main.go)

Run:

```bash
go run ./cmd/mmapplatform
```

## Current Matrix

| Platform family | mmap tree | Locks | reader table | owner tokens | file advice | cache/space stats | sparse punching |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Linux | Supported through `mmap` | `flock` exclusive writer and shared read-only locks | Supported | PID, process-start from `/proc/<pid>/stat`, boot token from `/proc/sys/kernel/random/boot_id` | `posix_fadvise` plus `madvise` | `mincore` and `stat(2)` | `fallocate(PUNCH_HOLE|KEEP_SIZE)` |
| Darwin | Supported through `mmap` | `flock` exclusive writer and shared read-only locks | Supported | PID, process-start from `kern.proc.pid`, boot token from `kern.boottime` | `madvise`; file advice is a no-op in this lab | `mincore` and `stat(2)` | Unsupported placeholder |
| Other Unix | Supported through `mmap` when Go and `x/sys/unix` provide the syscalls | `flock` exclusive writer and shared read-only locks | Supported | PID only; process-start and boot tokens are zero | `madvise`; file advice is a no-op in this lab | `mincore` and `stat(2)` | Unsupported placeholder |
| Non-Unix | Not supported for mmap-backed files | Stubbed | Stubbed | Stubbed | Stubbed | Zero-value stubs | Unsupported/inert |

The generic in-memory B-tree and page-backed in-memory B+tree are portable Go
code. This chapter is only about the mmap-backed file path.

```mermaid
flowchart TD
    P["MmapPlatformProfile"] --> U["unix profile"]
    P --> N["non-unix stub"]
    U --> L["linux: fadvise + fallocate punch"]
    U --> D["darwin: process/boot tokens"]
    U --> O["other unix: PID-only tokens"]
    P --> H["MmapHolePunchProfile"]
    P --> C["cmd/mmapplatform JSON"]
```

## What CI Proves

The GitHub Actions workflow has two layers:

- Native Linux `go test ./...` and `go vet ./...`.
- Cross-platform compile-only `go test -exec=/usr/bin/true ./...` for
  `linux/amd64`, `darwin/amd64`, `darwin/arm64`, `freebsd/amd64`, and
  `windows/amd64`.

The local equivalent is:

```bash
./scripts/ci-local.sh
```

Compile-only checks are still useful because build tags are part of the design:
Linux owns the active sparse-hole syscall, Darwin owns process and boot owner
tokens through `sysctl`, other Unix targets take PID-only fallbacks, and
non-Unix targets must keep the public API buildable through stubs. They do not
run the foreign test binaries, and they do not replace runtime testing on those
systems.

## Remaining Respectability Gap

A serious support matrix would need:

- Runtime tests on each supported operating system, not only cross-compilation.
- Filesystem-specific notes for ext4, XFS, APFS, ZFS, tmpfs, and network filesystems.
- Power-fail or VM-kill experiments per filesystem and mount option.
- Sparse-punch allocation evidence before and after maintenance on each filesystem.
- Long-running multi-process reader/writer soak runs outside `go test`.

Until that exists, the honest label is: **portable API, Unix-first mmap engine,
Linux-best sparse experiment, non-Unix stubs**.
