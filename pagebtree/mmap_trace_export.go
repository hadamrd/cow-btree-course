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
