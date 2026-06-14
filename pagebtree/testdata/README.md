# mmap fixtures

`mmap-v2-legacy-zero-key-order.db` is a small real mmap database image whose
metadata pages use the pre-key-order layout: the checksum-covered key-order word
is zero. `OpenMmap` treats zero as bytewise order for compatibility with clean
older images.

`mmap-v1-inline-freelist.db` is a real mmap database image with reusable page IDs
encoded directly in the metadata page's inline freelist area and the checked
metadata version rewritten to 1. It proves the reader can still open a pre-reclaim
metadata image, recover the inline reusable pages, and reuse one after a new
write.

`mmap-v2-chained-freelist.db` is a real mmap database image whose checked
version-2 metadata points at a multi-page `flagFreelist` chain. It proves the
reader can recover old pre-reclaim freelist pages, validate their checksums, and
reuse one of the recovered IDs after a later write.

Regenerate them from the repository root with:

```bash
go run ./pagebtree/testdata/generate_legacy_zero_key_order.go
go run ./pagebtree/testdata/generate_legacy_v1_inline_freelist.go
go run ./pagebtree/testdata/generate_legacy_v2_chained_freelist.go
```
