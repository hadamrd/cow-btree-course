package pagebtree

import (
	"bytes"
	"fmt"
	"slices"
	"testing"
)

type modelMap map[string][]byte

func TestPageTreeMatchesSortedMapModel(t *testing.T) {
	data := []byte{
		0, 1, 1,
		0, 2, 2,
		0, 3, 3,
		4, 2, 2, 5, 1, 3,
		1, 2, 0,
		3, 1, 3,
		2, 1, 0,
		0, 4, 4,
		5, 0, 4,
		3, 0, 5,
	}
	runPageTreeModel(t, data)
}

func FuzzPageTreeMatchesSortedMapModel(f *testing.F) {
	f.Add([]byte{0, 1, 1, 0, 2, 2, 1, 1, 3, 0, 3})
	f.Add([]byte{4, 2, 1, 0, 9, 5, 1, 9, 3, 0, 9})
	f.Add([]byte{0, 7, 1, 0, 8, 2, 5, 7, 9, 2, 8, 3, 0, 10})
	f.Fuzz(func(t *testing.T, data []byte) {
		runPageTreeModel(t, data)
	})
}

func runPageTreeModel(t *testing.T, data []byte) {
	t.Helper()

	tree := New(2)
	model := modelMap{}
	reader := modelReader{data: data}
	for step := 0; step < 96 && reader.hasMore(); step++ {
		op := reader.next() % 9
		switch op {
		case 0:
			key := reader.key()
			value := reader.value()
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
			count := int(reader.next()%4) + 1
			batch := tree.Batch()
			for i := 0; i < count; i++ {
				key := reader.key()
				if reader.next()%3 == 0 {
					batch.Delete(key)
					model.delete(key)
					continue
				}
				value := reader.value()
				batch.Put(key, value)
				model.put(key, value)
			}
			batch.Commit()
		case 5:
			start := reader.key()
			assertCursorFromMatchesModel(t, tree, model, start)
		case 6:
			start := reader.key()
			end := reader.key()
			if compareStrings(end, start) < 0 {
				start, end = end, start
			}
			assertCursorBetweenMatchesModel(t, tree, model, start, end)
		case 7:
			start := reader.key()
			end := reader.key()
			if compareStrings(end, start) < 0 {
				start, end = end, start
			}
			assertReverseCursorBetweenMatchesModel(t, tree, model, start, end)
		case 8:
			key := reader.key()
			cursor := tree.Cursor()
			if cursor.Seek(key) {
				deleteKey := cursor.Key()
				want, exists := model.delete(deleteKey)
				got, deleted := cursor.Delete()
				if deleted != exists || !bytes.Equal(got, want) {
					t.Fatalf("cursor Delete(%q) = %q, %v; want %q, %v", deleteKey, got, deleted, want, exists)
				}
				if cursor.Key() != deleteKey {
					t.Fatalf("cursor key after Delete = %q, want snapshot key %q", cursor.Key(), deleteKey)
				}
			}
			cursor.Close()
		}
		if err := tree.Check(); err != nil {
			t.Fatalf("Check after step %d: %v", step, err)
		}
		assertTreeMatchesModel(t, tree, model)
	}
}

type modelReader struct {
	data []byte
	pos  int
}

func (r *modelReader) hasMore() bool {
	return r.pos < len(r.data)
}

func (r *modelReader) next() byte {
	if len(r.data) == 0 {
		return 0
	}
	if r.pos >= len(r.data) {
		return 0
	}
	out := r.data[r.pos]
	r.pos++
	return out
}

func (r *modelReader) key() string {
	return fmt.Sprintf("key-%02d", r.next()%24)
}

func (r *modelReader) value() []byte {
	size := int(r.next()%17) + 1
	seed := r.next()
	value := make([]byte, size)
	for i := range value {
		value[i] = 'a' + byte((int(seed)+i)%26)
	}
	return value
}

func (m modelMap) put(key string, value []byte) {
	m[key] = cloneBytes(value)
}

func (m modelMap) delete(key string) ([]byte, bool) {
	value, ok := m[key]
	if !ok {
		return nil, false
	}
	delete(m, key)
	return cloneBytes(value), true
}

func (m modelMap) get(key string) ([]byte, bool) {
	value, ok := m[key]
	if !ok {
		return nil, false
	}
	return cloneBytes(value), true
}

func (m modelMap) keys() []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func assertTreeMatchesModel(t *testing.T, tree *Tree, model modelMap) {
	t.Helper()

	if got, want := tree.Len(), len(model); got != want {
		t.Fatalf("Len = %d, want %d", got, want)
	}
	for _, key := range model.keys() {
		got, ok := tree.Get(key)
		want := model[key]
		if !ok || !bytes.Equal(got, want) {
			t.Fatalf("Get(%q) = %q, %v; want %q, true", key, got, ok, want)
		}
	}
	var gotKeys []string
	tree.Range(func(key string, value []byte) bool {
		gotKeys = append(gotKeys, key)
		if want := model[key]; !bytes.Equal(value, want) {
			t.Fatalf("Range(%q) value = %q, want %q", key, value, want)
		}
		return true
	})
	if wantKeys := model.keys(); !slices.Equal(gotKeys, wantKeys) {
		t.Fatalf("Range keys = %v, want %v", gotKeys, wantKeys)
	}
}

func assertRangeBetweenMatchesModel(t *testing.T, tree *Tree, model modelMap, start, end string) {
	t.Helper()

	var got []string
	tree.RangeBetween(start, end, func(key string, value []byte) bool {
		got = append(got, key)
		if want := model[key]; !bytes.Equal(value, want) {
			t.Fatalf("RangeBetween(%q,%q) value for %q = %q, want %q", start, end, key, value, want)
		}
		return true
	})
	var want []string
	for _, key := range model.keys() {
		if compareStrings(key, start) >= 0 && compareStrings(key, end) < 0 {
			want = append(want, key)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("RangeBetween(%q,%q) keys = %v, want %v", start, end, got, want)
	}
}

func assertCursorFromMatchesModel(t *testing.T, tree *Tree, model modelMap, start string) {
	t.Helper()

	cursor := tree.Cursor()
	defer cursor.Close()
	var got []string
	for ok := cursor.Seek(start); ok; ok = cursor.Next() {
		key := cursor.Key()
		value := cursor.Value()
		got = append(got, key)
		if want := model[key]; !bytes.Equal(value, want) {
			t.Fatalf("cursor value for %q = %q, want %q", key, value, want)
		}
	}
	var want []string
	for _, key := range model.keys() {
		if compareStrings(key, start) >= 0 {
			want = append(want, key)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("cursor keys from %q = %v, want %v", start, got, want)
	}
}

func assertCursorBetweenMatchesModel(t *testing.T, tree *Tree, model modelMap, start, end string) {
	t.Helper()

	cursor := tree.CursorBetween(start, end)
	defer cursor.Close()
	var got []string
	for cursor.Valid() {
		key := cursor.Key()
		value := cursor.Value()
		got = append(got, key)
		if want := model[key]; !bytes.Equal(value, want) {
			t.Fatalf("bounded cursor value for %q in [%q,%q) = %q, want %q", key, start, end, value, want)
		}
		if !cursor.Next() {
			break
		}
	}
	var want []string
	for _, key := range model.keys() {
		if compareStrings(key, start) >= 0 && compareStrings(key, end) < 0 {
			want = append(want, key)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("bounded cursor keys for [%q,%q) = %v, want %v", start, end, got, want)
	}
}

func assertReverseCursorBetweenMatchesModel(t *testing.T, tree *Tree, model modelMap, start, end string) {
	t.Helper()

	cursor := tree.CursorBetween(start, end)
	defer cursor.Close()
	var got []string
	for ok := cursor.Last(); ok; ok = cursor.Prev() {
		key := cursor.Key()
		value := cursor.Value()
		got = append(got, key)
		if want := model[key]; !bytes.Equal(value, want) {
			t.Fatalf("reverse bounded cursor value for %q in [%q,%q) = %q, want %q", key, start, end, value, want)
		}
	}
	var want []string
	for _, key := range model.keys() {
		if compareStrings(key, start) >= 0 && compareStrings(key, end) < 0 {
			want = append(want, key)
		}
	}
	slices.Reverse(want)
	if !slices.Equal(got, want) {
		t.Fatalf("reverse bounded cursor keys for [%q,%q) = %v, want %v", start, end, got, want)
	}
}
