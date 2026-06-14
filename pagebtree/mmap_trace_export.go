package pagebtree

import (
	"encoding/json"
	"io"
	"sync"
)

// MmapTraceJSONLExporter writes mmap trace events as newline-delimited JSON.
//
// Trace hooks cannot return errors, so the exporter keeps the first write or
// encode error. Call Err after a traced operation, Sync, or Close to inspect
// whether exporting failed.
type MmapTraceJSONLExporter struct {
	mu     sync.Mutex
	writer io.Writer
	err    error
}

// NewMmapTraceJSONLExporter returns an exporter that writes one JSON event per
// line to writer. A nil writer is treated as io.Discard.
func NewMmapTraceJSONLExporter(writer io.Writer) *MmapTraceJSONLExporter {
	if writer == nil {
		writer = io.Discard
	}
	return &MmapTraceJSONLExporter{writer: writer}
}

// Hook returns a TraceHook-compatible callback that records events to the
// exporter.
func (e *MmapTraceJSONLExporter) Hook() MmapTraceHook {
	if e == nil {
		return func(MmapTraceEvent) {}
	}
	return e.Record
}

// Record writes event as one JSONL record unless an earlier export failed.
func (e *MmapTraceJSONLExporter) Record(event MmapTraceEvent) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return
	}
	line, err := json.Marshal(event)
	if err != nil {
		e.err = err
		return
	}
	line = append(line, '\n')
	written, err := e.writer.Write(line)
	if err != nil {
		e.err = err
		return
	}
	if written != len(line) {
		e.err = io.ErrShortWrite
	}
}

// Err returns the first error observed by the exporter, if any.
func (e *MmapTraceJSONLExporter) Err() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

// MmapTraceAsyncJSONLExporter writes mmap trace events as JSONL from a bounded
// background queue. When the queue is full, Record drops the event instead of
// blocking the storage-engine hook.
type MmapTraceAsyncJSONLExporter struct {
	exporter *MmapTraceJSONLExporter
	events   chan MmapTraceEvent
	wg       sync.WaitGroup

	mu      sync.Mutex
	closed  bool
	dropped uint64
}

// NewMmapTraceAsyncJSONLExporter returns an async JSONL exporter with a
// bounded event queue. A capacity below one is normalized to one.
func NewMmapTraceAsyncJSONLExporter(writer io.Writer, capacity int) *MmapTraceAsyncJSONLExporter {
	if capacity < 1 {
		capacity = 1
	}
	e := &MmapTraceAsyncJSONLExporter{
		exporter: NewMmapTraceJSONLExporter(writer),
		events:   make(chan MmapTraceEvent, capacity),
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for event := range e.events {
			e.exporter.Record(event)
		}
	}()
	return e
}

// Hook returns a TraceHook-compatible callback that queues events for export.
func (e *MmapTraceAsyncJSONLExporter) Hook() MmapTraceHook {
	if e == nil {
		return func(MmapTraceEvent) {}
	}
	return e.Record
}

// Record queues event for background JSONL export. If the queue is full, or if
// the exporter has been closed, the event is counted as dropped.
func (e *MmapTraceAsyncJSONLExporter) Record(event MmapTraceEvent) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		e.dropped++
		return
	}
	select {
	case e.events <- event:
	default:
		e.dropped++
	}
}

// Dropped returns the number of events that were not queued because the bounded
// queue was full or the exporter had already closed.
func (e *MmapTraceAsyncJSONLExporter) Dropped() uint64 {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dropped
}

// Close stops the background exporter after flushing queued events and returns
// the first JSONL encode/write error, if any. It is safe to call more than once.
func (e *MmapTraceAsyncJSONLExporter) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	if !e.closed {
		e.closed = true
		close(e.events)
	}
	e.mu.Unlock()
	e.wg.Wait()
	return e.Err()
}

// Err returns the first JSONL encode/write error observed by the background
// exporter, if any.
func (e *MmapTraceAsyncJSONLExporter) Err() error {
	if e == nil {
		return nil
	}
	return e.exporter.Err()
}
