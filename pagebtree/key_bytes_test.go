package pagebtree

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"
)

func TestTreeByteKeysPreserveOpaqueBytesAndOrdering(t *testing.T) {
	tree := New(2)
	keys := [][]byte{
		{0x00, 0x02},
		{0x00, 0xff},
		{0x01},
		{0xff, 0x00},
	}
	for i, key := range keys {
		tree.PutBytes(key, []byte(fmt.Sprintf("value-%d", i)))
	}

	key := []byte{0x00, 0xff}
	if got, ok := tree.GetBytes(key); !ok || string(got) != "value-1" {
		t.Fatalf("GetBytes(%x) = %q, %v; want value-1, true", key, got, ok)
	}

	var gotKeys [][]byte
	tree.RangeBytes(func(key []byte, value []byte) bool {
		gotKeys = append(gotKeys, append([]byte(nil), key...))
		key[0] = 0xee
		return true
	})
	if !equalByteKeySlices(gotKeys, keys) {
		t.Fatalf("RangeBytes keys = %x, want %x", gotKeys, keys)
	}

	var gotBounded [][]byte
	tree.RangeBytesBetween([]byte{0x00, 0x80}, []byte{0xff}, func(key []byte, value []byte) bool {
		gotBounded = append(gotBounded, append([]byte(nil), key...))
		return true
	})
	wantBounded := [][]byte{{0x00, 0xff}, {0x01}}
	if !equalByteKeySlices(gotBounded, wantBounded) {
		t.Fatalf("RangeBytesBetween keys = %x, want %x", gotBounded, wantBounded)
	}
}

func TestByteKeyAPIsCopyInputAndOutputKeys(t *testing.T) {
	tree := New(2)
	key := []byte{'k', 0x00, 0xff}
	tree.PutBytes(key, []byte("original"))
	key[0] = 'x'

	if got, ok := tree.GetBytes([]byte{'k', 0x00, 0xff}); !ok || string(got) != "original" {
		t.Fatalf("GetBytes after caller key mutation = %q, %v; want original, true", got, ok)
	}
	if _, ok := tree.GetBytes(key); ok {
		t.Fatalf("GetBytes with mutated key = true; want copied key storage")
	}

	cursor := tree.Cursor()
	defer cursor.Close()
	if !cursor.SeekBytes([]byte{'k'}) {
		t.Fatalf("SeekBytes(k) = false, want true")
	}
	gotKey := cursor.KeyBytes()
	gotKey[0] = 'x'
	if again := cursor.KeyBytes(); !bytes.Equal(again, []byte{'k', 0x00, 0xff}) {
		t.Fatalf("KeyBytes after caller mutation = %x, want original key", again)
	}
	cursor.Close()
	if got := cursor.KeyBytes(); got != nil {
		t.Fatalf("KeyBytes after Close = %x, want nil", got)
	}
}

func TestSnapshotAndBatchByteKeys(t *testing.T) {
	tree := New(2)
	tree.PutBytes([]byte{0x01}, []byte("one"))
	snapshot := tree.Snapshot()
	defer snapshot.Close()

	batch := tree.Batch()
	batch.PutBytes([]byte{0x02}, []byte("two"))
	batch.DeleteBytes([]byte{0x01})
	result, err := batch.CommitDetailed()
	if err != nil || !result.Changed {
		t.Fatalf("CommitDetailed byte batch = changed:%v err:%v; want changed nil", result.Changed, err)
	}
	if _, ok := tree.GetBytes([]byte{0x01}); ok {
		t.Fatalf("GetBytes(01) after byte batch delete = true, want false")
	}
	if got, ok := tree.GetBytes([]byte{0x02}); !ok || string(got) != "two" {
		t.Fatalf("GetBytes(02) after byte batch put = %q, %v; want two, true", got, ok)
	}
	if got, ok := snapshot.GetBytes([]byte{0x01}); !ok || string(got) != "one" {
		t.Fatalf("snapshot GetBytes(01) = %q, %v; want one, true", got, ok)
	}
	if _, ok := snapshot.GetBytes([]byte{0x02}); ok {
		t.Fatalf("snapshot GetBytes(02) = true; want old root")
	}
}

func TestMmapByteKeysPersistAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.PutBytes([]byte{0x00, 0xff}, []byte("high"))
	tree.PutBytes([]byte{0x00, 0x10}, []byte("low"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()

	var got [][]byte
	reopened.RangeBytes(func(key []byte, value []byte) bool {
		got = append(got, append([]byte(nil), key...))
		return true
	})
	want := [][]byte{{0x00, 0x10}, {0x00, 0xff}}
	if !equalByteKeySlices(got, want) {
		t.Fatalf("reopened RangeBytes keys = %x, want %x", got, want)
	}
	if got, ok := reopened.GetBytes([]byte{0x00, 0xff}); !ok || string(got) != "high" {
		t.Fatalf("reopened GetBytes(00ff) = %q, %v; want high, true", got, ok)
	}
}

func equalByteKeySlices(left, right [][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !bytes.Equal(left[i], right[i]) {
			return false
		}
	}
	return true
}
