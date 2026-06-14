package pagebtree

// MmapTraceEventKind names an mmap storage-engine decision or phase.
type MmapTraceEventKind string

const (
	MmapTraceSyncBegin                    MmapTraceEventKind = "mmap-sync-begin"
	MmapTraceSyncDataSynced               MmapTraceEventKind = "mmap-sync-data-synced"
	MmapTraceSyncMetaPublished            MmapTraceEventKind = "mmap-sync-meta-published"
	MmapTraceSyncEnd                      MmapTraceEventKind = "mmap-sync-end"
	MmapTraceRecoveryCandidateRejected    MmapTraceEventKind = "mmap-recovery-candidate-rejected"
	MmapTraceRecoveryCandidateAccepted    MmapTraceEventKind = "mmap-recovery-candidate-accepted"
	MmapTraceReclaimObsoleteMetadataPages MmapTraceEventKind = "mmap-reclaim-obsolete-metadata-pages"
)

// MmapTraceHook receives synchronous trace events from mmap-backed trees.
//
// The hook should return quickly and must not call back into the same tree.
type MmapTraceHook func(MmapTraceEvent)

// MmapTraceEvent describes a storage-engine phase using stable page/revision
// identifiers instead of formatted log text.
type MmapTraceEvent struct {
	Kind           MmapTraceEventKind
	Revision       uint64
	Root           PageID
	NextPage       PageID
	MaxPages       int
	Length         int
	DirtyPages     int
	FreePages      int
	RetiredPages   int
	ReclaimedPages int
	MetadataSlot   int
	Reason         string
}
