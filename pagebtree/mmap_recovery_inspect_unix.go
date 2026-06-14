//go:build unix

package pagebtree

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// InspectMmapRecovery reports metadata recovery candidate decisions without
// claiming a reader-table slot or mutating the database.
func InspectMmapRecovery(path string) ([]MmapTraceEvent, error) {
	events, _, err := inspectMmapRecovery(path)
	return events, err
}

func inspectMmapRecovery(path string) ([]MmapTraceEvent, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	if err := lockFile(file, false); err != nil {
		file.Close()
		return nil, 0, err
	}

	info, err := file.Stat()
	if err != nil {
		unlockFile(file)
		file.Close()
		return nil, 0, err
	}
	if err := validateExistingMmapFileSize(info.Size()); err != nil {
		unlockFile(file)
		file.Close()
		return nil, 0, err
	}

	size := int(info.Size())
	data, err := mmapBytes(int(file.Fd()), 0, size, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		unlockFile(file)
		file.Close()
		return nil, 0, err
	}
	arena := &mmapArena{
		file:     file,
		path:     path,
		data:     data,
		maxPages: int(info.Size()/PageSize) - metaPageCount,
		locked:   true,
		readOnly: true,
	}
	var events []MmapTraceEvent
	tree := &Tree{
		pages:                    map[PageID]*page{},
		nextPage:                 firstTreePageID,
		keyOrder:                 KeyOrderBytewise,
		keyComparator:            keyComparatorForOrder(KeyOrderBytewise),
		arena:                    arena,
		readOnly:                 true,
		pageCache:                newPageCache(DefaultPageCacheCapacity),
		rangePrefetchLeafWindow:  DefaultRangePrefetchLeafWindow,
		minRepairPageFillPercent: DefaultMinRepairPageFillPercent,
		traceHook: func(event MmapTraceEvent) {
			switch event.Kind {
			case MmapTraceRecoveryCandidateRejected, MmapTraceRecoveryCandidateAccepted:
				events = append(events, event)
			}
		},
	}
	if err := tree.loadMeta(); err != nil {
		return events, 0, errors.Join(err, arena.close())
	}
	revision := tree.revision
	if err := arena.close(); err != nil {
		return events, 0, err
	}
	return events, revision, nil
}
