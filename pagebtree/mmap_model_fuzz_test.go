//go:build unix

package pagebtree

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestMmapTreeMatchesSortedMapModelAcrossReopen(t *testing.T) {
	data := []byte{
		0, 1, 8, 2,
		0, 2, 9, 1,
		4, 2, 3, 0, 4, 12, 3,
		5, 0,
		1, 2,
		0, 5, 15, 9,
		6, 0,
		3, 1, 6,
		2, 5,
		5, 0,
	}
	runMmapTreeModel(t, data)
}

func FuzzMmapTreeMatchesSortedMapModelAcrossReopen(f *testing.F) {
	f.Add([]byte{0, 1, 8, 2, 5, 0, 0, 2, 4, 1, 6, 0})
	f.Add([]byte{4, 3, 1, 0, 7, 15, 8, 1, 7, 5, 0, 3, 0, 9})
	f.Add([]byte{0, 3, 255, 4, 6, 0, 0, 4, 254, 9, 5, 0, 1, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		runMmapTreeModel(t, data)
	})
}

func runMmapTreeModel(t *testing.T, data []byte) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "course.db")
	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 32})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer func() {
		_ = tree.Close()
	}()

	model := modelMap{}
	reader := modelReader{data: data}
	for step := 0; step < 64 && reader.hasMore(); step++ {
		op := reader.next() % 9
		switch op {
		case 0:
			key := reader.key()
			value := reader.mmapValue()
			tree.Put(key, value)
			model.put(key, value)
		case 1:
			key := reader.key()
			got, ok := tree.Delete(key)
			want, exists := model.delete(key)
			if ok != exists || !bytes.Equal(got, want) {
				t.Fatalf("Delete(%q) = %q, %v; want %q, %v", key, got, ok, want, exists)
			}
		case 2:
			key := reader.key()
			got, ok := tree.Get(key)
			want, exists := model.get(key)
			if ok != exists || !bytes.Equal(got, want) {
				t.Fatalf("Get(%q) = %q, %v; want %q, %v", key, got, ok, want, exists)
			}
		case 3:
			start := reader.key()
			end := reader.key()
			if compareStrings(end, start) < 0 {
				start, end = end, start
			}
			assertRangeBetweenMatchesModel(t, tree, model, start, end)
		case 4:
			count := int(reader.next()%3) + 1
			batch := tree.Batch()
			for i := 0; i < count; i++ {
				key := reader.key()
				if reader.next()%4 == 0 {
					batch.Delete(key)
					model.delete(key)
					continue
				}
				value := reader.mmapValue()
				batch.Put(key, value)
				model.put(key, value)
			}
			batch.Commit()
		case 5:
			tree = syncCloseReopenMmapModel(t, tree, path, model)
		case 6:
			if err := tree.Sync(); err != nil {
				t.Fatalf("Sync at step %d: %v", step, err)
			}
		case 7:
			start := reader.key()
			end := reader.key()
			if compareStrings(end, start) < 0 {
				start, end = end, start
			}
			batch := tree.Batch()
			batch.DeleteRange(start, end)
			for _, key := range model.keys() {
				if compareStrings(key, start) >= 0 && compareStrings(key, end) < 0 {
					model.delete(key)
				}
			}
			batch.Commit()
		case 8:
			count := int(reader.next()%3) + 1
			tx := tree.BeginReadWrite()
			txModel := model.clone()
			for i := 0; i < count; i++ {
				switch reader.next() % 4 {
				case 0:
					key := reader.key()
					tx.Delete(key)
					txModel.delete(key)
				case 1:
					start := reader.key()
					end := reader.key()
					if compareStrings(end, start) < 0 {
						start, end = end, start
					}
					tx.DeleteRange(start, end)
					txModel.deleteRange(start, end)
				default:
					key := reader.key()
					value := reader.mmapValue()
					tx.Put(key, value)
					txModel.put(key, value)
				}
				key := reader.key()
				got, ok := tx.Get(key)
				want, exists := txModel.get(key)
				if ok != exists || !bytes.Equal(got, want) {
					t.Fatalf("mmap tx Get(%q) = %q, %v; want %q, %v", key, got, ok, want, exists)
				}
			}
			start := reader.key()
			end := reader.key()
			if compareStrings(end, start) < 0 {
				start, end = end, start
			}
			assertTxRangeBetweenMatchesModel(t, tx, txModel, start, end)
			tx.Commit()
			model = txModel
		}
		if err := tree.Check(); err != nil {
			t.Fatalf("Check after mmap step %d: %v", step, err)
		}
		assertTreeMatchesModel(t, tree, model)
	}
	tree = syncCloseReopenMmapModel(t, tree, path, model)
}

func syncCloseReopenMmapModel(t *testing.T, tree *Tree, path string, model modelMap) *Tree {
	t.Helper()

	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync before reopen: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close before reopen: %v", err)
	}
	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	assertTreeMatchesModel(t, reopened, model)
	if err := reopened.Check(); err != nil {
		reopened.Close()
		t.Fatalf("Check after reopen: %v", err)
	}
	return reopened
}

func (r *modelReader) mmapValue() []byte {
	value := r.value()
	if r.next()%5 != 0 {
		return value
	}
	size := PageSize + int(r.next()%64)
	seed := r.next()
	large := make([]byte, size)
	for i := range large {
		large[i] = 'A' + byte((int(seed)+i)%26)
	}
	return large
}
