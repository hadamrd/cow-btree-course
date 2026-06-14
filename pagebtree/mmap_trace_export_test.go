package pagebtree

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestMmapTraceJSONLExporterWritesStableFields(t *testing.T) {
	var buf bytes.Buffer
	exporter := NewMmapTraceJSONLExporter(&buf)
	hook := exporter.Hook()

	hook(MmapTraceEvent{
		Kind:          MmapTraceSyncDataRange,
		Revision:      7,
		Root:          3,
		NextPage:      12,
		StartPage:     5,
		EndPage:       9,
		MaxPages:      64,
		DurationNanos: 12345,
		DirtyPages:    4,
		MetadataSlot:  -1,
	})
	hook(MmapTraceEvent{
		Kind:     MmapTraceSyncEnd,
		Revision: 7,
		Root:     3,
		NextPage: 12,
		MaxPages: 64,
	})
	if err := exporter.Err(); err != nil {
		t.Fatalf("exporter Err = %v, want nil", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("exported %d JSONL records, want 2: %q", len(lines), buf.String())
	}
	first := decodeTraceJSONLine(t, lines[0])
	if first["kind"] != string(MmapTraceSyncDataRange) {
		t.Fatalf("kind = %v, want %q", first["kind"], MmapTraceSyncDataRange)
	}
	if first["revision"] != float64(7) || first["root"] != float64(3) || first["next_page"] != float64(12) {
		t.Fatalf("revision/root/next_page fields = %#v", first)
	}
	if first["start_page"] != float64(5) || first["end_page"] != float64(9) {
		t.Fatalf("range fields = %#v", first)
	}
	if first["duration_nanos"] != float64(12345) || first["dirty_pages"] != float64(4) {
		t.Fatalf("duration/dirty fields = %#v", first)
	}
	if _, ok := first["metadata_slot"]; ok {
		t.Fatalf("exported no-slot sentinel metadata_slot: %#v", first)
	}

	second := decodeTraceJSONLine(t, lines[1])
	if second["kind"] != string(MmapTraceSyncEnd) {
		t.Fatalf("second kind = %v, want %q", second["kind"], MmapTraceSyncEnd)
	}
	if _, ok := second["start_page"]; ok {
		t.Fatalf("sync-end record included empty start_page: %#v", second)
	}
}

func TestMmapTraceJSONLExporterRetainsWriteError(t *testing.T) {
	exporter := NewMmapTraceJSONLExporter(errorWriter{err: errTraceWriterBoom})
	exporter.Record(MmapTraceEvent{Kind: MmapTraceSyncBegin})
	exporter.Record(MmapTraceEvent{Kind: MmapTraceSyncEnd})

	if err := exporter.Err(); !errors.Is(err, errTraceWriterBoom) {
		t.Fatalf("exporter Err = %v, want %v", err, errTraceWriterBoom)
	}
}

func TestMmapTraceJSONLExporterDetectsShortWrite(t *testing.T) {
	exporter := NewMmapTraceJSONLExporter(shortWriter{})
	exporter.Record(MmapTraceEvent{Kind: MmapTraceSyncBegin})

	if err := exporter.Err(); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("exporter Err = %v, want ErrShortWrite", err)
	}
}

func TestMmapTraceAsyncJSONLExporterFlushesOnClose(t *testing.T) {
	var buf bytes.Buffer
	exporter := NewMmapTraceAsyncJSONLExporter(&buf, 4)
	hook := exporter.Hook()

	hook(MmapTraceEvent{Kind: MmapTraceSyncBegin, Revision: 1})
	hook(MmapTraceEvent{Kind: MmapTraceSyncEnd, Revision: 1})

	if err := exporter.Close(); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}
	if exporter.Dropped() != 0 {
		t.Fatalf("Dropped = %d, want 0", exporter.Dropped())
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("exported %d JSONL records, want 2: %q", len(lines), buf.String())
	}
	if got := decodeTraceJSONLine(t, lines[0])["kind"]; got != string(MmapTraceSyncBegin) {
		t.Fatalf("first kind = %v, want %q", got, MmapTraceSyncBegin)
	}
	if got := decodeTraceJSONLine(t, lines[1])["kind"]; got != string(MmapTraceSyncEnd) {
		t.Fatalf("second kind = %v, want %q", got, MmapTraceSyncEnd)
	}
}

func TestMmapTraceAsyncJSONLExporterDropsWhenBufferIsFull(t *testing.T) {
	writer := newBlockingTraceWriter()
	exporter := NewMmapTraceAsyncJSONLExporter(writer, 1)
	hook := exporter.Hook()

	hook(MmapTraceEvent{Kind: MmapTraceSyncBegin})
	<-writer.started

	hook(MmapTraceEvent{Kind: MmapTraceSyncDataSynced})
	hook(MmapTraceEvent{Kind: MmapTraceSyncEnd})

	if exporter.Dropped() != 1 {
		t.Fatalf("Dropped = %d, want 1", exporter.Dropped())
	}
	writer.Release()
	if err := exporter.Close(); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}
	lines := strings.Split(strings.TrimSpace(writer.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("exported %d JSONL records, want 2: %q", len(lines), writer.String())
	}
}

func TestMmapTraceAsyncJSONLExporterRetainsWriteError(t *testing.T) {
	exporter := NewMmapTraceAsyncJSONLExporter(errorWriter{err: errTraceWriterBoom}, 1)
	exporter.Record(MmapTraceEvent{Kind: MmapTraceSyncBegin})

	if err := exporter.Close(); !errors.Is(err, errTraceWriterBoom) {
		t.Fatalf("Close = %v, want %v", err, errTraceWriterBoom)
	}
	if err := exporter.Err(); !errors.Is(err, errTraceWriterBoom) {
		t.Fatalf("Err = %v, want %v", err, errTraceWriterBoom)
	}
}

func decodeTraceJSONLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("decode JSONL record %q: %v", line, err)
	}
	return record
}

var errTraceWriterBoom = errors.New("trace writer boom")

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	return len(p) - 1, nil
}

type blockingTraceWriter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	buf     bytes.Buffer
	mu      sync.Mutex
}

func newBlockingTraceWriter() *blockingTraceWriter {
	return &blockingTraceWriter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (w *blockingTraceWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *blockingTraceWriter) Release() {
	close(w.release)
}

func (w *blockingTraceWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
