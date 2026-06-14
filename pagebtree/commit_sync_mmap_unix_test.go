//go:build unix

package pagebtree

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestMmapWriteBatchCommitSyncDetailedSyncsChangedCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")
	var events []MmapTraceEvent

	tree, err := OpenMmap(path, MmapOptions{
		Degree:   2,
		MaxPages: 128,
		TraceHook: func(event MmapTraceEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	beforeRevision := tree.Revision()
	events = nil

	batch := tree.Batch()
	batch.Put("bravo", []byte("two"))
	result, err := batch.CommitSyncDetailed()
	if err != nil {
		t.Fatalf("CommitSyncDetailed error: %v", err)
	}
	if !result.Changed || len(result.Operations) != 1 {
		t.Fatalf("CommitSyncDetailed result = %+v, want one changed operation", result)
	}
	if got := tree.Revision(); got != beforeRevision+1 {
		t.Fatalf("Revision after CommitSyncDetailed = %d, want %d", got, beforeRevision+1)
	}
	if traceKindIndex(events, MmapTraceSyncEnd) < 0 {
		t.Fatalf("CommitSyncDetailed trace events = %v, want sync end", events)
	}
	if got := tree.Stats().SyncedRevision; got != tree.Revision() {
		t.Fatalf("SyncedRevision after CommitSyncDetailed = %d, want logical revision %d", got, tree.Revision())
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after CommitSyncDetailed: %v", err)
	}
}

func TestMmapWriteBatchCommitSyncDetailedSkipsSyncForNoOpCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")
	var events []MmapTraceEvent

	tree, err := OpenMmap(path, MmapOptions{
		Degree:   2,
		MaxPages: 128,
		TraceHook: func(event MmapTraceEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	beforeRevision := tree.Revision()
	events = nil

	result, err := tree.Batch().CommitSyncDetailed()
	if err != nil {
		t.Fatalf("empty CommitSyncDetailed error: %v", err)
	}
	if result.Changed || len(result.Operations) != 0 {
		t.Fatalf("empty CommitSyncDetailed result = %+v, want no change", result)
	}
	if got := tree.Revision(); got != beforeRevision {
		t.Fatalf("Revision after empty CommitSyncDetailed = %d, want %d", got, beforeRevision)
	}
	if traceKindIndex(events, MmapTraceSyncBegin) >= 0 {
		t.Fatalf("empty CommitSyncDetailed trace events = %v, want no sync", events)
	}
}

func TestMmapWriteBatchCommitSyncDetailedReturnsSyncErrorWithResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer func() {
		tree.arena.faultInjector = nil
		_ = tree.Close()
	}()
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}

	forced := fmt.Errorf("forced %s fault", mmapFaultBeforeDataSync)
	tree.arena.faultInjector = func(point mmapFaultPoint) error {
		if point == mmapFaultBeforeDataSync {
			return forced
		}
		return nil
	}
	batch := tree.Batch()
	batch.Put("bravo", []byte("two"))
	result, err := batch.CommitSyncDetailed()
	if !errors.Is(err, forced) {
		t.Fatalf("CommitSyncDetailed error = %v, want forced sync error", err)
	}
	if !result.Changed || len(result.Operations) != 1 {
		t.Fatalf("CommitSyncDetailed result after sync error = %+v, want one changed operation", result)
	}
	if got, ok := tree.Get("bravo"); !ok || string(got) != "two" {
		t.Fatalf("Get(bravo) after CommitSyncDetailed sync error = %q, %v; want logical commit visible", got, ok)
	}
	if got, want := tree.Stats().SyncedRevision, uint64(1); got != want {
		t.Fatalf("SyncedRevision after CommitSyncDetailed sync error = %d, want previous synced revision %d", got, want)
	}
}

func TestMmapStatsSyncedRevisionTracksSyncAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	if got := tree.Stats().SyncedRevision; got != tree.Revision() {
		t.Fatalf("new mmap SyncedRevision = %d, want initial revision %d", got, tree.Revision())
	}

	tree.Put("alpha", []byte("one"))
	afterPutRevision := tree.Revision()
	if got := tree.Stats().SyncedRevision; got == afterPutRevision {
		t.Fatalf("SyncedRevision after unsynced Put = %d, want older than logical revision %d", got, afterPutRevision)
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync after Put: %v", err)
	}
	if got := tree.Stats().SyncedRevision; got != afterPutRevision {
		t.Fatalf("SyncedRevision after Sync = %d, want %d", got, afterPutRevision)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close after Sync: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()
	if got := reopened.Stats().SyncedRevision; got != reopened.Revision() {
		t.Fatalf("reopened SyncedRevision = %d, want recovered revision %d", got, reopened.Revision())
	}
}

func TestMmapWriteBatchCommitSyncDetailedFaultsRemainRetryable(t *testing.T) {
	tests := []struct {
		name  string
		fault mmapFaultPoint
	}{
		{
			name:  "before data sync",
			fault: mmapFaultBeforeDataSync,
		},
		{
			name:  "after metadata write",
			fault: mmapFaultAfterMetaWrite,
		},
		{
			name:  "before metadata sync",
			fault: mmapFaultBeforeMetaSync,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "course.db")
			assertCommitSyncRetryPublishesAfterFault(t, path, tt.fault)
		})
	}
}

func assertCommitSyncRetryPublishesAfterFault(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	var events []MmapTraceEvent
	tree, err := OpenMmap(path, MmapOptions{
		Degree:   2,
		MaxPages: 128,
		TraceHook: func(event MmapTraceEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	events = nil

	forced := fmt.Errorf("forced %s fault", fault)
	tree.arena.faultInjector = func(point mmapFaultPoint) error {
		if point == fault {
			return forced
		}
		return nil
	}
	batch := tree.Batch()
	batch.Put("bravo", []byte("two"))
	result, err := batch.CommitSyncDetailed()
	if !errors.Is(err, forced) {
		t.Fatalf("CommitSyncDetailed fault %s error = %v, want forced error", fault, err)
	}
	if !result.Changed || len(result.Operations) != 1 {
		t.Fatalf("CommitSyncDetailed fault %s result = %+v, want one changed operation", fault, result)
	}
	if got, ok := tree.Get("bravo"); !ok || string(got) != "two" {
		t.Fatalf("Get(bravo) after CommitSyncDetailed fault %s = %q, %v; want logical commit visible", fault, got, ok)
	}
	if traceKindIndex(events, MmapTraceSyncFailed) < 0 {
		t.Fatalf("CommitSyncDetailed fault %s trace events = %v, want sync failed", fault, events)
	}

	tree.arena.faultInjector = nil
	if err := tree.Sync(); err != nil {
		t.Fatalf("retry Sync after CommitSyncDetailed fault %s: %v", fault, err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close after retry Sync fault %s: %v", fault, err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen after retry Sync fault %s: %v", fault, err)
	}
	defer reopened.Close()
	if got, ok := reopened.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("reopened Get(alpha) after retry Sync fault %s = %q, %v; want one, true", fault, got, ok)
	}
	if got, ok := reopened.Get("bravo"); !ok || string(got) != "two" {
		t.Fatalf("reopened Get(bravo) after retry Sync fault %s = %q, %v; want two, true", fault, got, ok)
	}
}

func TestMmapReadWriteTxCommitSyncDetailedSyncsChangedCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")
	var events []MmapTraceEvent

	tree, err := OpenMmap(path, MmapOptions{
		Degree:   3,
		MaxPages: 128,
		TraceHook: func(event MmapTraceEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	beforeRevision := tree.Revision()
	events = nil

	tx := tree.BeginReadWrite()
	tx.Put("bravo", []byte("two"))
	result, err := tx.CommitSyncDetailed()
	if err != nil {
		t.Fatalf("tx CommitSyncDetailed error: %v", err)
	}
	if !result.Changed || len(result.Operations) != 1 {
		t.Fatalf("tx CommitSyncDetailed result = %+v, want one changed operation", result)
	}
	if got := tree.Revision(); got != beforeRevision+1 {
		t.Fatalf("Revision after tx CommitSyncDetailed = %d, want %d", got, beforeRevision+1)
	}
	if traceKindIndex(events, MmapTraceSyncEnd) < 0 {
		t.Fatalf("tx CommitSyncDetailed trace events = %v, want sync end", events)
	}
	if got, ok := tree.Get("bravo"); !ok || string(got) != "two" {
		t.Fatalf("Get(bravo) after tx CommitSyncDetailed = %q, %v; want two, true", got, ok)
	}
}

func TestMmapReadWriteTxCommitSyncDetailedFaultsRemainRetryable(t *testing.T) {
	tests := []struct {
		name  string
		fault mmapFaultPoint
	}{
		{
			name:  "before data sync",
			fault: mmapFaultBeforeDataSync,
		},
		{
			name:  "after metadata write",
			fault: mmapFaultAfterMetaWrite,
		},
		{
			name:  "before metadata sync",
			fault: mmapFaultBeforeMetaSync,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "course.db")
			assertTxCommitSyncRetryPublishesAfterFault(t, path, tt.fault)
		})
	}
}

func assertTxCommitSyncRetryPublishesAfterFault(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	var events []MmapTraceEvent
	tree, err := OpenMmap(path, MmapOptions{
		Degree:   2,
		MaxPages: 128,
		TraceHook: func(event MmapTraceEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	tree.Put("remove", []byte("gone"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	events = nil

	forced := fmt.Errorf("forced %s fault", fault)
	tree.arena.faultInjector = func(point mmapFaultPoint) error {
		if point == fault {
			return forced
		}
		return nil
	}
	tx := tree.BeginReadWrite()
	tx.Put("bravo", []byte("two"))
	tx.Delete("remove")
	result, err := tx.CommitSyncDetailed()
	if !errors.Is(err, forced) {
		t.Fatalf("tx CommitSyncDetailed fault %s error = %v, want forced error", fault, err)
	}
	if !result.Changed || len(result.Operations) != 2 {
		t.Fatalf("tx CommitSyncDetailed fault %s result = %+v, want two changed operations", fault, result)
	}
	if got, ok := tree.Get("bravo"); !ok || string(got) != "two" {
		t.Fatalf("Get(bravo) after tx CommitSyncDetailed fault %s = %q, %v; want logical commit visible", fault, got, ok)
	}
	if got, ok := tree.Get("remove"); ok {
		t.Fatalf("Get(remove) after tx CommitSyncDetailed fault %s = %q, true; want logical delete visible", fault, got)
	}
	if traceKindIndex(events, MmapTraceSyncFailed) < 0 {
		t.Fatalf("tx CommitSyncDetailed fault %s trace events = %v, want sync failed", fault, events)
	}

	tree.arena.faultInjector = nil
	if err := tree.Sync(); err != nil {
		t.Fatalf("retry Sync after tx CommitSyncDetailed fault %s: %v", fault, err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close after tx retry Sync fault %s: %v", fault, err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen after tx retry Sync fault %s: %v", fault, err)
	}
	defer reopened.Close()
	if got, ok := reopened.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("reopened Get(alpha) after tx retry Sync fault %s = %q, %v; want one, true", fault, got, ok)
	}
	if got, ok := reopened.Get("bravo"); !ok || string(got) != "two" {
		t.Fatalf("reopened Get(bravo) after tx retry Sync fault %s = %q, %v; want two, true", fault, got, ok)
	}
	if got, ok := reopened.Get("remove"); ok {
		t.Fatalf("reopened Get(remove) after tx retry Sync fault %s = %q, true; want deleted", fault, got)
	}
}
