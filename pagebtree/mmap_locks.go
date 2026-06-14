package pagebtree

// MmapLockStats reports passive lock-sidecar evidence for an mmap database.
//
// WriterSidecarExists means the writer mutex file exists. WriterLocked means a
// non-blocking exclusive lock attempt observed active writer contention.
type MmapLockStats struct {
	WriterSidecarExists bool `json:"writer_sidecar_exists"`
	WriterLocked        bool `json:"writer_locked"`
}
