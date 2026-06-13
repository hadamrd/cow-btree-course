# mmap fixtures

`mmap-v2-legacy-zero-key-order.db` is a small real mmap database image whose
metadata pages use the pre-key-order layout: the checksum-covered key-order word
is zero. `OpenMmap` treats zero as bytewise order for compatibility with clean
older images.

Regenerate it from the repository root with:

```bash
go run ./pagebtree/testdata/generate_legacy_zero_key_order.go
```
