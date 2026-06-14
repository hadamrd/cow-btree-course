package pagebtree

import "encoding/json"

// MmapTraceEventKind names an mmap storage-engine decision or phase.
type MmapTraceEventKind string

const (
	MmapTraceSyncBegin                    MmapTraceEventKind = "mmap-sync-begin"
	MmapTraceSyncDataRange                MmapTraceEventKind = "mmap-sync-data-range"
	MmapTraceSyncDataSynced               MmapTraceEventKind = "mmap-sync-data-synced"
	MmapTraceSyncMetaPublished            MmapTraceEventKind = "mmap-sync-meta-published"
	MmapTraceSyncEnd                      MmapTraceEventKind = "mmap-sync-end"
	MmapTraceSyncFailed                   MmapTraceEventKind = "mmap-sync-failed"
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
	Kind               MmapTraceEventKind `json:"kind"`
	Revision           uint64             `json:"revision,omitempty"`
	Root               PageID             `json:"root,omitempty"`
	NextPage           PageID             `json:"next_page,omitempty"`
	StartPage          PageID             `json:"start_page,omitempty"`
	EndPage            PageID             `json:"end_page,omitempty"`
	MaxPages           int                `json:"max_pages,omitempty"`
	OldNextPage        PageID             `json:"old_next_page,omitempty"`
	NewNextPage        PageID             `json:"new_next_page,omitempty"`
	OldMaxPages        int                `json:"old_max_pages,omitempty"`
	NewMaxPages        int                `json:"new_max_pages,omitempty"`
	FileSizeBytes      int64              `json:"file_size_bytes,omitempty"`
	DurationNanos      int64              `json:"duration_nanos,omitempty"`
	Length             int                `json:"length,omitempty"`
	DirtyPages         int                `json:"dirty_pages,omitempty"`
	FreePages          int                `json:"free_pages,omitempty"`
	RetiredPages       int                `json:"retired_pages,omitempty"`
	ReclaimedPages     int                `json:"reclaimed_pages,omitempty"`
	ClearedReaderSlots int                `json:"cleared_reader_slots,omitempty"`
	MetadataSlot       int                `json:"metadata_slot,omitempty"`
	Reason             string             `json:"reason,omitempty"`
}

type mmapTraceEventJSON struct {
	Kind               MmapTraceEventKind `json:"kind"`
	Revision           uint64             `json:"revision,omitempty"`
	Root               PageID             `json:"root,omitempty"`
	NextPage           PageID             `json:"next_page,omitempty"`
	StartPage          PageID             `json:"start_page,omitempty"`
	EndPage            PageID             `json:"end_page,omitempty"`
	MaxPages           int                `json:"max_pages,omitempty"`
	OldNextPage        PageID             `json:"old_next_page,omitempty"`
	NewNextPage        PageID             `json:"new_next_page,omitempty"`
	OldMaxPages        int                `json:"old_max_pages,omitempty"`
	NewMaxPages        int                `json:"new_max_pages,omitempty"`
	FileSizeBytes      int64              `json:"file_size_bytes,omitempty"`
	DurationNanos      int64              `json:"duration_nanos,omitempty"`
	Length             int                `json:"length,omitempty"`
	DirtyPages         int                `json:"dirty_pages,omitempty"`
	FreePages          int                `json:"free_pages,omitempty"`
	RetiredPages       int                `json:"retired_pages,omitempty"`
	ReclaimedPages     int                `json:"reclaimed_pages,omitempty"`
	ClearedReaderSlots int                `json:"cleared_reader_slots,omitempty"`
	MetadataSlot       *int               `json:"metadata_slot,omitempty"`
	Reason             string             `json:"reason,omitempty"`
}

// MarshalJSON exports trace events with stable lower-snake field names. A
// negative MetadataSlot is an internal "not applicable" sentinel and is omitted.
func (event MmapTraceEvent) MarshalJSON() ([]byte, error) {
	record := mmapTraceEventJSON{
		Kind:               event.Kind,
		Revision:           event.Revision,
		Root:               event.Root,
		NextPage:           event.NextPage,
		StartPage:          event.StartPage,
		EndPage:            event.EndPage,
		MaxPages:           event.MaxPages,
		OldNextPage:        event.OldNextPage,
		NewNextPage:        event.NewNextPage,
		OldMaxPages:        event.OldMaxPages,
		NewMaxPages:        event.NewMaxPages,
		FileSizeBytes:      event.FileSizeBytes,
		DurationNanos:      event.DurationNanos,
		Length:             event.Length,
		DirtyPages:         event.DirtyPages,
		FreePages:          event.FreePages,
		RetiredPages:       event.RetiredPages,
		ReclaimedPages:     event.ReclaimedPages,
		ClearedReaderSlots: event.ClearedReaderSlots,
		Reason:             event.Reason,
	}
	if event.MetadataSlot >= 0 {
		record.MetadataSlot = &event.MetadataSlot
	}
	return json.Marshal(record)
}
