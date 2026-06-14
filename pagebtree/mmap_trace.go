package pagebtree

// MmapTraceEventKind names an mmap storage-engine decision or phase.
type MmapTraceEventKind string

const (
	MmapTraceSyncBegin                    MmapTraceEventKind = "mmap-sync-begin"
	MmapTraceSyncDataRange                MmapTraceEventKind = "mmap-sync-data-range"
	MmapTraceSyncDataSynced               MmapTraceEventKind = "mmap-sync-data-synced"
	MmapTraceSyncMetaPublished            MmapTraceEventKind = "mmap-sync-meta-published"
	MmapTraceSyncEnd                      MmapTraceEventKind = "mmap-sync-end"
	MmapTraceRecoveryCandidateRejected    MmapTraceEventKind = "mmap-recovery-candidate-rejected"
	MmapTraceRecoveryCandidateAccepted    MmapTraceEventKind = "mmap-recovery-candidate-accepted"
	MmapTraceReclaimObsoleteMetadataPages MmapTraceEventKind = "mmap-reclaim-obsolete-metadata-pages"
	MmapTraceGrowthBegin                  MmapTraceEventKind = "mmap-growth-begin"
	MmapTraceGrowthEnd                    MmapTraceEventKind = "mmap-growth-end"
	MmapTraceCompactBegin                 MmapTraceEventKind = "mmap-compact-begin"
	MmapTraceCompactEnd                   MmapTraceEventKind = "mmap-compact-end"
	MmapTraceReaderTableCleanup           MmapTraceEventKind = "mmap-reader-table-cleanup"
)

// MmapTraceHook receives synchronous trace events from mmap-backed trees.
//
// The hook should return quickly and must not call back into the same tree.
type MmapTraceHook func(MmapTraceEvent)

// MmapTraceEvent describes a storage-engine phase using stable page/revision
// identifiers instead of formatted log text.
type MmapTraceEvent struct {
	Kind               MmapTraceEventKind
	Revision           uint64
	Root               PageID
	NextPage           PageID
	StartPage          PageID
	EndPage            PageID
	MaxPages           int
	OldNextPage        PageID
	NewNextPage        PageID
	OldMaxPages        int
	NewMaxPages        int
	FileSizeBytes      int64
	DurationNanos      int64
	Length             int
	DirtyPages         int
	FreePages          int
	RetiredPages       int
	ReclaimedPages     int
	ClearedReaderSlots int
	MetadataSlot       int
	Reason             string
}
