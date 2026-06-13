package pagebtree

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const (
	benchmarkKeyCount   = 4096
	benchmarkRangeWidth = 128
	benchmarkCursorStep = 16
)

var (
	benchmarkValueSink []byte
	benchmarkIntSink   int
)

func BenchmarkPageTreeGet(b *testing.B) {
	tree, keys := benchmarkPageTree(b, benchmarkKeyCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value, ok := tree.Get(keys[i&(benchmarkKeyCount-1)])
		if !ok {
			b.Fatalf("Get missed benchmark key")
		}
		benchmarkValueSink = value
	}
}

func BenchmarkPageTreeCursorSeekNext(b *testing.B) {
	tree, keys := benchmarkPageTree(b, benchmarkKeyCount)
	cursor := tree.Cursor()
	defer cursor.Close()

	total := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		index := (i * benchmarkCursorStep) & (benchmarkKeyCount - 1)
		if !cursor.Seek(keys[index]) {
			b.Fatalf("Seek missed benchmark key")
		}
		total++
		for step := 1; step < benchmarkCursorStep && cursor.Next(); step++ {
			total++
		}
	}
	b.ReportMetric(float64(total)/float64(b.N), "keys/op")
	benchmarkIntSink = total
}

func BenchmarkPageTreeRangeBetween(b *testing.B) {
	tree, keys := benchmarkPageTree(b, benchmarkKeyCount)

	total := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := (i * benchmarkRangeWidth) % (benchmarkKeyCount - benchmarkRangeWidth)
		end := start + benchmarkRangeWidth
		tree.RangeBetween(keys[start], keys[end], func(key string, value []byte) bool {
			total++
			return true
		})
	}
	b.ReportMetric(float64(total)/float64(b.N), "keys/op")
	benchmarkIntSink = total
}

func BenchmarkPageTreePutSequential(b *testing.B) {
	keys := benchmarkKeys(b.N)
	value := benchmarkValue()
	tree := NewWithOptions(2, Options{PageCacheCapacity: DefaultPageCacheCapacity})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Put(keys[i], value)
	}
	benchmarkIntSink = tree.Len()
}

func BenchmarkPageTreeDeleteSequential(b *testing.B) {
	keys := benchmarkKeys(benchmarkKeyCount)
	value := benchmarkValue()
	tree := benchmarkPageTreeFromKeys(keys, value)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i > 0 && i%benchmarkKeyCount == 0 {
			b.StopTimer()
			tree = benchmarkPageTreeFromKeys(keys, value)
			b.StartTimer()
		}
		_, deleted := tree.Delete(keys[i&(benchmarkKeyCount-1)])
		if !deleted {
			b.Fatalf("Delete missed benchmark key")
		}
	}
	benchmarkIntSink = tree.Len()
}

func BenchmarkMmapTreeGet(b *testing.B) {
	tree, keys := benchmarkMmapTree(b, benchmarkKeyCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value, ok := tree.Get(keys[i&(benchmarkKeyCount-1)])
		if !ok {
			b.Fatalf("Get missed benchmark key")
		}
		benchmarkValueSink = value
	}
}

func BenchmarkMmapTreeRangeBetween(b *testing.B) {
	tree, keys := benchmarkMmapTree(b, benchmarkKeyCount)

	total := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := (i * benchmarkRangeWidth) % (benchmarkKeyCount - benchmarkRangeWidth)
		end := start + benchmarkRangeWidth
		tree.RangeBetween(keys[start], keys[end], func(key string, value []byte) bool {
			total++
			return true
		})
	}
	b.ReportMetric(float64(total)/float64(b.N), "keys/op")
	benchmarkIntSink = total
}

func BenchmarkMmapTreePutSync(b *testing.B) {
	path := filepath.Join(b.TempDir(), "bench.db")
	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 8192})
	if err != nil {
		b.Fatalf("OpenMmap: %v", err)
	}
	defer tree.Close()

	keys := benchmarkKeys(b.N)
	value := benchmarkValue()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Put(keys[i], value)
		if err := tree.Sync(); err != nil {
			b.Fatalf("Sync: %v", err)
		}
	}
	benchmarkIntSink = tree.Len()
}

func BenchmarkMmapTreeDeleteSync(b *testing.B) {
	path := filepath.Join(b.TempDir(), "bench.db")
	keys := benchmarkKeys(benchmarkKeyCount)
	value := benchmarkValue()
	tree := benchmarkMmapTreeFromKeys(b, path, keys, value)
	b.Cleanup(func() {
		if err := tree.Close(); err != nil {
			b.Fatalf("Close benchmark mmap delete tree: %v", err)
		}
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i > 0 && i%benchmarkKeyCount == 0 {
			b.StopTimer()
			if err := tree.Close(); err != nil {
				b.Fatalf("Close refill tree: %v", err)
			}
			tree = benchmarkMmapTreeFromKeys(b, path, keys, value)
			b.StartTimer()
		}
		_, deleted := tree.Delete(keys[i&(benchmarkKeyCount-1)])
		if !deleted {
			b.Fatalf("Delete missed benchmark key")
		}
		if err := tree.Sync(); err != nil {
			b.Fatalf("Sync: %v", err)
		}
	}
	benchmarkIntSink = tree.Len()
}

func BenchmarkMmapTreeOpenExisting(b *testing.B) {
	path := filepath.Join(b.TempDir(), "bench.db")
	seed, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 8192})
	if err != nil {
		b.Fatalf("OpenMmap seed: %v", err)
	}
	keys := benchmarkKeys(benchmarkKeyCount)
	value := benchmarkValue()
	for _, key := range keys {
		seed.Put(key, value)
	}
	if err := seed.Close(); err != nil {
		b.Fatalf("Close seed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree, err := OpenMmap(path, MmapOptions{})
		if err != nil {
			b.Fatalf("OpenMmap existing: %v", err)
		}
		if err := tree.Close(); err != nil {
			b.Fatalf("Close existing: %v", err)
		}
	}
}

func benchmarkPageTree(b *testing.B, count int) (*Tree, []string) {
	b.Helper()

	keys := benchmarkKeys(count)
	value := benchmarkValue()
	tree := benchmarkPageTreeFromKeys(keys, value)
	return tree, keys
}

func benchmarkPageTreeFromKeys(keys []string, value []byte) *Tree {
	tree := NewWithOptions(2, Options{PageCacheCapacity: DefaultPageCacheCapacity})
	for _, key := range keys {
		tree.Put(key, value)
	}
	return tree
}

func benchmarkMmapTree(b *testing.B, count int) (*Tree, []string) {
	b.Helper()

	path := filepath.Join(b.TempDir(), "bench.db")
	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 8192})
	if err != nil {
		b.Fatalf("OpenMmap: %v", err)
	}
	b.Cleanup(func() {
		if err := tree.Close(); err != nil {
			b.Fatalf("Close benchmark mmap tree: %v", err)
		}
	})

	keys := benchmarkKeys(count)
	value := benchmarkValue()
	for _, key := range keys {
		tree.Put(key, value)
	}
	if err := tree.Sync(); err != nil {
		b.Fatalf("Sync benchmark mmap tree: %v", err)
	}
	return tree, keys
}

func benchmarkMmapTreeFromKeys(b *testing.B, path string, keys []string, value []byte) *Tree {
	b.Helper()

	_ = removeMmapFiles(path)
	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 8192})
	if err != nil {
		b.Fatalf("OpenMmap: %v", err)
	}
	for _, key := range keys {
		tree.Put(key, value)
	}
	if err := tree.Sync(); err != nil {
		_ = tree.Close()
		b.Fatalf("Sync benchmark mmap tree: %v", err)
	}
	return tree
}

func removeMmapFiles(path string) error {
	if err := removeIfExists(path); err != nil {
		return err
	}
	if err := removeIfExists(path + ".readers"); err != nil {
		return err
	}
	return removeIfExists(path + ".writer")
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func benchmarkKeys(count int) []string {
	keys := make([]string, count)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%08d", i)
	}
	return keys
}

func benchmarkValue() []byte {
	return bytes.Repeat([]byte("v"), 64)
}
