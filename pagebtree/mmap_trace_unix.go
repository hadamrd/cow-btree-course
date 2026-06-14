//go:build unix

package pagebtree

func (t *Tree) emitMmapTrace(kind MmapTraceEventKind) {
	t.emitMmapTraceEvent(kind, nil, -1, "")
}

func (t *Tree) emitMmapTraceRecord(kind MmapTraceEventKind, record metaRecord, slot int, reason string) {
	t.emitMmapTraceEvent(kind, &record, slot, reason)
}

func (t *Tree) emitMmapTraceReclaimed(kind MmapTraceEventKind, reclaimedPages int) {
	if t == nil || t.traceHook == nil {
		return
	}
	event := t.mmapTraceEvent(kind, nil, -1, "")
	event.ReclaimedPages = reclaimedPages
	t.traceHook(event)
}

func (t *Tree) emitMmapTraceMetadataRollback(err error) {
	if t == nil || t.traceHook == nil || err == nil || len(t.metaFreelistPages) == 0 {
		return
	}
	kind := MmapTraceFreelistMetadataRollback
	if p := t.pages[t.metaFreelistPages[0]]; p != nil && p.flags() == flagReclaim {
		kind = MmapTraceReclaimMetadataRollback
	}
	event := t.mmapTraceEvent(kind, nil, -1, err.Error())
	event.MetadataPages = len(t.metaFreelistPages)
	event.StartPage = t.metaFreelistPages[0]
	event.EndPage = t.metaFreelistPages[0] + 1
	for _, id := range t.metaFreelistPages[1:] {
		if id < event.StartPage {
			event.StartPage = id
		}
		if id >= event.EndPage {
			event.EndPage = id + 1
		}
	}
	t.traceHook(event)
}

func (t *Tree) emitMmapTraceDataRange(startPage, endPage PageID, durationNanos int64) {
	if t == nil || t.traceHook == nil {
		return
	}
	event := t.mmapTraceEvent(MmapTraceSyncDataRange, nil, -1, "")
	event.StartPage = startPage
	event.EndPage = endPage
	event.DurationNanos = durationNanos
	t.traceHook(event)
}

func (t *Tree) emitMmapTraceFailure(kind MmapTraceEventKind, err error) {
	if t == nil || t.traceHook == nil || err == nil {
		return
	}
	event := t.mmapTraceEvent(kind, nil, -1, err.Error())
	t.traceHook(event)
}

func (t *Tree) emitMmapTraceResize(kind MmapTraceEventKind, oldMaxPages, newMaxPages int, oldNextPage, newNextPage PageID, fileSizeBytes int64) {
	if t == nil || t.traceHook == nil {
		return
	}
	event := t.mmapTraceEvent(kind, nil, -1, "")
	event.OldMaxPages = oldMaxPages
	event.NewMaxPages = newMaxPages
	event.OldNextPage = oldNextPage
	event.NewNextPage = newNextPage
	event.FileSizeBytes = fileSizeBytes
	t.traceHook(event)
}

func (t *Tree) emitMmapTraceResizeFailure(kind MmapTraceEventKind, oldMaxPages, newMaxPages int, oldNextPage, newNextPage PageID, fileSizeBytes int64, err error) {
	if t == nil || t.traceHook == nil || err == nil {
		return
	}
	event := t.mmapTraceEvent(kind, nil, -1, err.Error())
	event.OldMaxPages = oldMaxPages
	event.NewMaxPages = newMaxPages
	event.OldNextPage = oldNextPage
	event.NewNextPage = newNextPage
	event.FileSizeBytes = fileSizeBytes
	t.traceHook(event)
}

func (t *Tree) emitMmapTraceReaderCleanup(clearedSlots int) {
	if t == nil || t.traceHook == nil || clearedSlots == 0 {
		return
	}
	event := t.mmapTraceEvent(MmapTraceReaderTableCleanup, nil, -1, "")
	event.ClearedReaderSlots = clearedSlots
	t.traceHook(event)
}

func (t *Tree) emitMmapTraceMetaError(kind MmapTraceEventKind, metaPage []byte, slot int, err error) {
	if t == nil || t.traceHook == nil {
		return
	}
	revision, ok := trustedMetaRevision(metaPage)
	event := t.mmapTraceEvent(kind, nil, slot, err.Error())
	if ok {
		event.Revision = revision
	}
	t.traceHook(event)
}

func (t *Tree) emitMmapTracePunch(kind MmapTraceEventKind, stats MmapHolePunchStats, reason string) {
	if t == nil || t.traceHook == nil {
		return
	}
	event := t.mmapTraceEvent(kind, nil, -1, reason)
	event.FreePages = stats.FreePages
	event.SkippedRecoverablePages = stats.SkippedRecoverablePages
	event.PunchRanges = stats.Ranges
	event.PunchedPages = stats.PunchedPages
	event.PunchedBytes = stats.PunchedBytes
	t.traceHook(event)
}

func (t *Tree) emitMmapTracePunchRange(startPage, endPage PageID) {
	if t == nil || t.traceHook == nil {
		return
	}
	event := t.mmapTraceEvent(MmapTracePunchRange, nil, -1, "")
	event.StartPage = startPage
	event.EndPage = endPage
	pages := int(endPage - startPage)
	event.PunchedPages = pages
	event.PunchedBytes = int64(pages * PageSize)
	t.traceHook(event)
}

func (t *Tree) emitMmapTracePunchFailure(stats MmapHolePunchStats, startPage, endPage PageID, err error) {
	if t == nil || t.traceHook == nil || err == nil {
		return
	}
	event := t.mmapTraceEvent(MmapTracePunchFailed, nil, -1, err.Error())
	event.StartPage = startPage
	event.EndPage = endPage
	event.FreePages = stats.FreePages
	event.SkippedRecoverablePages = stats.SkippedRecoverablePages
	event.PunchRanges = stats.Ranges
	event.PunchedPages = stats.PunchedPages
	event.PunchedBytes = stats.PunchedBytes
	t.traceHook(event)
}

func (t *Tree) mmapTraceEvent(kind MmapTraceEventKind, record *metaRecord, slot int, reason string) MmapTraceEvent {
	event := MmapTraceEvent{
		Kind:         kind,
		Revision:     t.revision,
		Root:         t.root,
		NextPage:     t.nextPage,
		Length:       t.length,
		FreePages:    len(t.free),
		RetiredPages: len(t.retired),
		MetadataSlot: slot,
		Reason:       reason,
	}
	if t.arena != nil {
		event.MaxPages = t.arena.maxPages
		event.DirtyPages = len(t.arena.dirtyPages)
	}
	if record != nil {
		event.Revision = record.revision
		event.Root = record.root
		event.NextPage = record.nextPage
		event.MaxPages = record.maxPages
		event.Length = record.length
		event.FreePages = len(record.free)
		event.RetiredPages = len(record.retired)
	}
	return event
}

func (t *Tree) emitMmapTraceEvent(kind MmapTraceEventKind, record *metaRecord, slot int, reason string) {
	if t == nil || t.traceHook == nil {
		return
	}
	t.traceHook(t.mmapTraceEvent(kind, record, slot, reason))
}
