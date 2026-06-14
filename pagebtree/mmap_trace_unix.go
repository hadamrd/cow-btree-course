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
