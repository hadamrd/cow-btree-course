package pagebtree

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
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
