package pagebtree

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestReadWriteTxReadsOwnWritesAndPublishesOneRevision(t *testing.T) {
	tree := New(2)
	tree.Put("alpha", []byte("one"))
	tree.Put("bravo", []byte("two"))
	beforeRevision := tree.Revision()
	snapshot := tree.Snapshot()
	defer snapshot.Close()

	tx := tree.BeginReadWrite()
	if got, ok := tx.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("tx Get(alpha) before staged write = %q, %v; want one, true", got, ok)
	}
	tx.Put("alpha", []byte("updated"))
	tx.Put("charlie", []byte("three"))
	tx.Delete("bravo")

	if got, ok := tree.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("tree Get(alpha) before tx Commit = %q, %v; want old value", got, ok)
	}
	if got, ok := tx.Get("alpha"); !ok || string(got) != "updated" {
		t.Fatalf("tx Get(alpha) after staged put = %q, %v; want updated, true", got, ok)
	}
	if got, ok := tx.Get("charlie"); !ok || string(got) != "three" {
		t.Fatalf("tx Get(charlie) after staged put = %q, %v; want three, true", got, ok)
	}
	if got, ok := tx.Get("bravo"); ok {
		t.Fatalf("tx Get(bravo) after staged delete = %q, true; want hidden", got)
	}

	result, err := tx.CommitDetailed()
	if err != nil {
		t.Fatalf("tx CommitDetailed: %v", err)
	}
	if !result.Changed {
		t.Fatalf("tx CommitDetailed Changed = false, want true")
	}
	if got := tree.Revision(); got != beforeRevision+1 {
		t.Fatalf("Revision after tx Commit = %d, want %d", got, beforeRevision+1)
	}
	if got, ok := tree.Get("alpha"); !ok || string(got) != "updated" {
		t.Fatalf("tree Get(alpha) after tx Commit = %q, %v; want updated, true", got, ok)
	}
	if _, ok := tree.Get("bravo"); ok {
		t.Fatalf("tree Get(bravo) after tx Commit = true; want deleted")
	}
	if got, ok := tree.Get("charlie"); !ok || string(got) != "three" {
		t.Fatalf("tree Get(charlie) after tx Commit = %q, %v; want three, true", got, ok)
	}
	if got, ok := snapshot.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("snapshot Get(alpha) after tx Commit = %q, %v; want one, true", got, ok)
	}
}

func TestReadWriteTxCommitSyncReturnsChanged(t *testing.T) {
	tree := New(2)
	tree.Put("alpha", []byte("one"))

	tx := tree.BeginReadWrite()
	tx.Put("bravo", []byte("two"))
	changed, err := tx.CommitSync()
	if err != nil {
		t.Fatalf("tx CommitSync error: %v", err)
	}
	if !changed {
		t.Fatalf("tx CommitSync changed = false, want true")
	}
	if got, ok := tree.Get("bravo"); !ok || string(got) != "two" {
		t.Fatalf("Get(bravo) after tx CommitSync = %q, %v; want two, true", got, ok)
	}
}

func TestReadWriteTxDeleteRangeUsesTransactionView(t *testing.T) {
	tree := New(3)
	for i := 0; i < 8; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	tx := tree.BeginReadWrite()
	tx.Put("key-03-extra", []byte("staged"))
	tx.Delete("key-04")
	tx.DeleteRange("key-02", "key-05")
	tx.Put("key-03-late", []byte("late"))

	if _, ok := tx.Get("key-02"); ok {
		t.Fatalf("tx Get(key-02) after DeleteRange = true; want false")
	}
	if _, ok := tx.Get("key-03-extra"); ok {
		t.Fatalf("tx Get(key-03-extra) after DeleteRange = true; want false")
	}
	if _, ok := tx.Get("key-04"); ok {
		t.Fatalf("tx Get(key-04) after DeleteRange = true; want false")
	}
	if got, ok := tx.Get("key-03-late"); !ok || string(got) != "late" {
		t.Fatalf("tx Get(key-03-late) after late put = %q, %v; want late, true", got, ok)
	}
	if got, ok := tree.Get("key-03-extra"); ok {
		t.Fatalf("tree Get(key-03-extra) before tx Commit = %q, true; want staged key hidden", got)
	}

	if changed := tx.Commit(); !changed {
		t.Fatalf("tx Commit changed = false, want true")
	}
	for _, key := range []string{"key-02", "key-03", "key-03-extra", "key-04"} {
		if _, ok := tree.Get(key); ok {
			t.Fatalf("tree Get(%s) after tx range delete = true; want false", key)
		}
	}
	if got, ok := tree.Get("key-03-late"); !ok || string(got) != "late" {
		t.Fatalf("tree Get(key-03-late) after tx Commit = %q, %v; want late, true", got, ok)
	}
	if got, ok := tree.Get("key-05"); !ok || string(got) != "value-05" {
		t.Fatalf("tree Get(key-05) after tx Commit = %q, %v; want value-05, true", got, ok)
	}
}

func TestReadWriteTxRangeBetweenUsesTransactionView(t *testing.T) {
	tree := New(3)
	for _, key := range []string{"alpha", "bravo", "charlie", "delta", "echo"} {
		tree.Put(key, []byte(key+"-base"))
	}

	tx := tree.BeginReadWrite()
	tx.Put("bravo", []byte("bravo-staged"))
	tx.Delete("charlie")
	tx.Put("cobalt", []byte("cobalt-staged"))

	var got []string
	tx.RangeBetween("bravo", "echo", func(key string, value []byte) bool {
		got = append(got, key+"="+string(value))
		return true
	})
	want := []string{
		"bravo=bravo-staged",
		"cobalt=cobalt-staged",
		"delta=delta-base",
	}
	if len(got) != len(want) {
		t.Fatalf("tx RangeBetween rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tx RangeBetween row %d = %s, want %s; all rows %v", i, got[i], want[i], got)
		}
	}
}

func TestReadWriteTxByteKeysUseTransactionView(t *testing.T) {
	tree := New(2)
	tree.PutBytes([]byte{0x01}, []byte("one"))
	tree.PutBytes([]byte{0x02}, []byte("two"))
	tree.PutBytes([]byte{0x03}, []byte("three"))

	tx := tree.BeginReadWrite()
	tx.PutBytes([]byte{0x02}, []byte("two-staged"))
	tx.PutBytes([]byte{0x04}, []byte("four"))
	tx.DeleteBytesRange([]byte{0x01}, []byte{0x03})

	if _, ok := tx.GetBytes([]byte{0x01}); ok {
		t.Fatalf("tx GetBytes(01) after byte range delete = true; want false")
	}
	if _, ok := tx.GetBytes([]byte{0x02}); ok {
		t.Fatalf("tx GetBytes(02) after byte range delete = true; want false")
	}
	if got, ok := tx.GetBytes([]byte{0x04}); !ok || string(got) != "four" {
		t.Fatalf("tx GetBytes(04) = %q, %v; want four, true", got, ok)
	}

	if changed := tx.Commit(); !changed {
		t.Fatalf("byte-key tx Commit changed = false, want true")
	}
	if _, ok := tree.GetBytes([]byte{0x01}); ok {
		t.Fatalf("tree GetBytes(01) after byte-key tx = true; want false")
	}
	if _, ok := tree.GetBytes([]byte{0x02}); ok {
		t.Fatalf("tree GetBytes(02) after byte-key tx = true; want false")
	}
	if got, ok := tree.GetBytes([]byte{0x04}); !ok || string(got) != "four" {
		t.Fatalf("tree GetBytes(04) after byte-key tx = %q, %v; want four, true", got, ok)
	}
}

func TestReadWriteTxCursorBetweenDeletesInsideTransaction(t *testing.T) {
	tree := New(3)
	for i := 0; i < 7; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	beforeRevision := tree.Revision()

	tx := tree.BeginReadWrite()
	tx.Put("key-03-extra", []byte("staged"))
	tx.Delete("key-04")

	cursor := tx.CursorBetween("key-02", "key-06")
	defer cursor.Close()
	if !cursor.Valid() || cursor.Key() != "key-02" {
		t.Fatalf("tx cursor first key = %q valid=%v; want key-02 true", cursor.Key(), cursor.Valid())
	}
	old, deleted := cursor.Delete()
	if !deleted || string(old) != "value-02" {
		t.Fatalf("tx cursor Delete = %q, %v; want value-02, true", old, deleted)
	}
	if old, deleted := cursor.Delete(); deleted || old != nil {
		t.Fatalf("second tx cursor Delete = %q, %v; want nil, false", old, deleted)
	}
	if got, ok := tx.Get("key-02"); ok {
		t.Fatalf("tx Get(key-02) after cursor Delete = %q, true; want hidden", got)
	}
	if got, ok := tree.Get("key-02"); !ok || string(got) != "value-02" {
		t.Fatalf("tree Get(key-02) before tx Commit = %q, %v; want value-02, true", got, ok)
	}
	if cursor.Key() != "key-02" {
		t.Fatalf("tx cursor key after Delete = %q, want original cursor key", cursor.Key())
	}
	if !cursor.Next() || cursor.Key() != "key-03" {
		t.Fatalf("tx cursor Next after Delete key = %q valid=%v; want key-03 true", cursor.Key(), cursor.Valid())
	}
	if !cursor.Next() || cursor.Key() != "key-03-extra" {
		t.Fatalf("tx cursor Next staged key = %q valid=%v; want key-03-extra true", cursor.Key(), cursor.Valid())
	}
	if !cursor.Next() || cursor.Key() != "key-05" {
		t.Fatalf("tx cursor skipped deleted live key incorrectly, got %q valid=%v; want key-05 true", cursor.Key(), cursor.Valid())
	}
	if cursor.Next() {
		t.Fatalf("tx cursor moved past exclusive end; got %q", cursor.Key())
	}

	if changed := tx.Commit(); !changed {
		t.Fatalf("tx Commit after cursor Delete changed = false, want true")
	}
	if got := tree.Revision(); got != beforeRevision+1 {
		t.Fatalf("Revision after tx cursor Commit = %d, want %d", got, beforeRevision+1)
	}
	if _, ok := tree.Get("key-02"); ok {
		t.Fatalf("tree Get(key-02) after tx cursor Commit = true; want deleted")
	}
	if _, ok := tree.Get("key-04"); ok {
		t.Fatalf("tree Get(key-04) after tx cursor Commit = true; want deleted")
	}
	if got, ok := tree.Get("key-03-extra"); !ok || string(got) != "staged" {
		t.Fatalf("tree Get(key-03-extra) after tx cursor Commit = %q, %v; want staged, true", got, ok)
	}
}

func TestReadWriteTxCursorSeekLastPrevAndByteKeys(t *testing.T) {
	tree := New(2)
	tree.PutBytes([]byte{0x01}, []byte("one"))
	tree.PutBytes([]byte{0x03}, []byte("three"))
	tree.PutBytes([]byte{0x05}, []byte("five"))

	tx := tree.BeginReadWrite()
	tx.PutBytes([]byte{0x04}, []byte("four"))
	tx.DeleteBytes([]byte{0x03})

	cursor := tx.CursorBytesBetween([]byte{0x01}, []byte{0x06})
	defer cursor.Close()
	if !cursor.SeekBytes([]byte{0x02}) || string(cursor.KeyBytes()) != string([]byte{0x04}) {
		t.Fatalf("tx byte cursor SeekBytes(02) key = %x valid=%v; want 04 true", cursor.KeyBytes(), cursor.Valid())
	}
	if !cursor.Last() || string(cursor.KeyBytes()) != string([]byte{0x05}) {
		t.Fatalf("tx byte cursor Last key = %x valid=%v; want 05 true", cursor.KeyBytes(), cursor.Valid())
	}
	if !cursor.Prev() || string(cursor.KeyBytes()) != string([]byte{0x04}) {
		t.Fatalf("tx byte cursor Prev key = %x valid=%v; want 04 true", cursor.KeyBytes(), cursor.Valid())
	}
	old, deleted := cursor.Delete()
	if !deleted || string(old) != "four" {
		t.Fatalf("tx byte cursor Delete = %q, %v; want four, true", old, deleted)
	}
	if _, ok := tx.GetBytes([]byte{0x04}); ok {
		t.Fatalf("tx GetBytes(04) after byte cursor Delete = true; want false")
	}
}

func TestReadWriteTxUsesStableBaseRevisionAndRejectsConflict(t *testing.T) {
	tree := New(2)
	tree.Put("alpha", []byte("one"))
	tree.Put("bravo", []byte("two"))
	beforeRevision := tree.Revision()

	tx := tree.BeginReadWrite()
	tx.Put("charlie", []byte("three"))
	tree.Put("alpha", []byte("outside"))
	tree.Put("delta", []byte("outside"))

	if got, ok := tx.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("tx Get(alpha) after external write = %q, %v; want stable base one, true", got, ok)
	}
	if _, ok := tx.Get("delta"); ok {
		t.Fatalf("tx Get(delta) after external insert = true; want hidden from stable base")
	}
	var got []string
	tx.RangeBetween("alpha", "echo", func(key string, value []byte) bool {
		got = append(got, key)
		return true
	})
	want := []string{"alpha", "bravo", "charlie"}
	if len(got) != len(want) {
		t.Fatalf("tx RangeBetween after external write = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tx RangeBetween row %d = %s, want %s; all rows %v", i, got[i], want[i], got)
		}
	}

	result, err := tx.CommitDetailed()
	if !errors.Is(err, ErrTxConflict) {
		t.Fatalf("tx CommitDetailed conflict error = %v, want ErrTxConflict", err)
	}
	if result.Changed {
		t.Fatalf("conflicted tx Changed = true, want false")
	}
	if got := tree.Revision(); got != beforeRevision+2 {
		t.Fatalf("Revision after conflicted tx = %d, want external writes only %d", got, beforeRevision+2)
	}
	if _, ok := tree.Get("charlie"); ok {
		t.Fatalf("tree Get(charlie) after conflicted tx = true; want tx write discarded")
	}
	if got, ok := tree.Get("alpha"); !ok || string(got) != "outside" {
		t.Fatalf("tree Get(alpha) after conflicted tx = %q, %v; want outside, true", got, ok)
	}
}

func TestReadWriteTxRollbackReleasesStableBaseReader(t *testing.T) {
	tree := New(2)
	tree.Put("alpha", []byte("one"))

	tx := tree.BeginReadWrite()
	if got := tree.Stats().ActiveReaders; got != 1 {
		t.Fatalf("ActiveReaders after BeginReadWrite = %d, want 1", got)
	}
	tx.Rollback()
	if got := tree.Stats().ActiveReaders; got != 0 {
		t.Fatalf("ActiveReaders after tx Rollback = %d, want 0", got)
	}

	empty := tree.BeginReadWrite()
	if got := tree.Stats().ActiveReaders; got != 1 {
		t.Fatalf("ActiveReaders after empty BeginReadWrite = %d, want 1", got)
	}
	if changed := empty.Commit(); changed {
		t.Fatalf("empty tx Commit changed = true, want false")
	}
	if got := tree.Stats().ActiveReaders; got != 0 {
		t.Fatalf("ActiveReaders after empty tx Commit = %d, want 0", got)
	}
}

func TestReadWriteTxRollbackAndEmptyCommitDoNotPublishRevision(t *testing.T) {
	tree := New(2)
	tree.Put("alpha", []byte("one"))
	beforeRevision := tree.Revision()

	rolledBack := tree.BeginReadWrite()
	rolledBack.Put("alpha", []byte("two"))
	rolledBack.Rollback()
	if changed := rolledBack.Commit(); changed {
		t.Fatalf("Commit after tx Rollback changed = true, want false")
	}
	if got := tree.Revision(); got != beforeRevision {
		t.Fatalf("Revision after tx Rollback = %d, want %d", got, beforeRevision)
	}
	if got, ok := tree.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("tree Get(alpha) after tx Rollback = %q, %v; want one, true", got, ok)
	}

	empty := tree.BeginReadWrite()
	if changed := empty.Commit(); changed {
		t.Fatalf("empty tx Commit changed = true, want false")
	}
	if got := tree.Revision(); got != beforeRevision {
		t.Fatalf("Revision after empty tx Commit = %d, want %d", got, beforeRevision)
	}
}

func TestMmapReadWriteTxPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 3, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 10; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	beforeRevision := tree.Revision()

	tx := tree.BeginReadWrite()
	tx.Put("key-04-extra", []byte("staged"))
	tx.DeleteRange("key-03", "key-06")
	tx.Put("key-05-late", []byte("late"))
	if changed := tx.Commit(); !changed {
		t.Fatalf("tx Commit changed = false, want true")
	}
	if got := tree.Revision(); got != beforeRevision+1 {
		t.Fatalf("Revision after mmap tx Commit = %d, want %d", got, beforeRevision+1)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after mmap tx Commit: %v", err)
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync after mmap tx Commit: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close after mmap tx Commit: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()
	for _, key := range []string{"key-03", "key-04", "key-04-extra", "key-05"} {
		if _, ok := reopened.Get(key); ok {
			t.Fatalf("reopened Get(%s) = true; want deleted", key)
		}
	}
	if got, ok := reopened.Get("key-05-late"); !ok || string(got) != "late" {
		t.Fatalf("reopened Get(key-05-late) = %q, %v; want late, true", got, ok)
	}
}

func TestMmapReadWriteTxCursorDeletePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 3, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 8; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	beforeRevision := tree.Revision()

	tx := tree.BeginReadWrite()
	tx.Put("key-03-extra", []byte("staged"))
	cursor := tx.CursorBetween("key-02", "key-05")
	if !cursor.Seek("key-03") {
		t.Fatalf("tx cursor Seek(key-03) = false, want true")
	}
	old, deleted := cursor.Delete()
	cursor.Close()
	if !deleted || string(old) != "value-03" {
		t.Fatalf("tx cursor Delete key-03 = %q, %v; want value-03, true", old, deleted)
	}
	if changed := tx.Commit(); !changed {
		t.Fatalf("tx Commit after cursor Delete changed = false, want true")
	}
	if got := tree.Revision(); got != beforeRevision+1 {
		t.Fatalf("Revision after mmap tx cursor Commit = %d, want %d", got, beforeRevision+1)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after mmap tx cursor Commit: %v", err)
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync after mmap tx cursor Commit: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close after mmap tx cursor Commit: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()
	if _, ok := reopened.Get("key-03"); ok {
		t.Fatalf("reopened Get(key-03) = true; want cursor-deleted")
	}
	if got, ok := reopened.Get("key-03-extra"); !ok || string(got) != "staged" {
		t.Fatalf("reopened Get(key-03-extra) = %q, %v; want staged, true", got, ok)
	}
}

func TestMmapReadWriteTxRejectsConflictAndReleasesReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}

	tx := tree.BeginReadWrite()
	tx.Put("bravo", []byte("two"))
	tree.Put("alpha", []byte("outside"))
	if got := tree.Stats().ActiveReaders; got != 1 {
		t.Fatalf("ActiveReaders before conflicted tx Commit = %d, want 1", got)
	}
	if got, ok := tx.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("tx Get(alpha) after external mmap write = %q, %v; want stable base one, true", got, ok)
	}
	result, err := tx.CommitDetailed()
	if !errors.Is(err, ErrTxConflict) {
		t.Fatalf("mmap tx CommitDetailed conflict error = %v, want ErrTxConflict", err)
	}
	if result.Changed {
		t.Fatalf("conflicted mmap tx Changed = true, want false")
	}
	if got := tree.Stats().ActiveReaders; got != 0 {
		t.Fatalf("ActiveReaders after conflicted tx Commit = %d, want 0", got)
	}
	if _, ok := tree.Get("bravo"); ok {
		t.Fatalf("tree Get(bravo) after conflicted mmap tx = true; want tx write discarded")
	}
	if got, ok := tree.Get("alpha"); !ok || string(got) != "outside" {
		t.Fatalf("tree Get(alpha) after conflicted mmap tx = %q, %v; want outside, true", got, ok)
	}
}
