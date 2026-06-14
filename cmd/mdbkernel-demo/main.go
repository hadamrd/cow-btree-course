package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func main() {
	path := filepath.Join(os.TempDir(), "cow-btree-mdb-kernel.db")
	_ = os.Remove(path)
	_ = os.Remove(path + ".readers")
	_ = os.Remove(path + ".writer")

	writer, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{
		Degree:            2,
		MaxPages:          64,
		PageCacheCapacity: 16,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer writer.Close()

	for i := 0; i < 18; i++ {
		writer.Put(fmt.Sprintf("uid=%02d", i), []byte(fmt.Sprintf("entry-%02d", i)))
	}
	if err := writer.Sync(); err != nil {
		log.Fatal(err)
	}

	reader, err := pagebtree.OpenMmapReadOnly(path)
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	writer.Put("uid=03", []byte("entry-03-updated"))
	writer.Delete("uid=07")
	if err := writer.Check(); err != nil {
		log.Fatal(err)
	}

	readerValue, readerOK := reader.Get("uid=03")
	writerValue, writerOK := writer.Get("uid=03")
	writerValue, writerOK = writer.Get("uid=03")
	deletedValue, deletedOK := reader.Get("uid=07")
	readerStats, err := writer.MmapReaderStats()
	if err != nil {
		log.Fatal(err)
	}

	profile := writer.MDBKernelProfile()
	fmt.Printf("db: %s\n", path)
	printProfile(os.Stdout, profile)
	fmt.Printf("reader table: %+v\n", readerStats)
	fmt.Printf("read-only uid=03: %q ok=%v\n", readerValue, readerOK)
	fmt.Printf("writer uid=03:    %q ok=%v\n", writerValue, writerOK)
	fmt.Printf("read-only uid=07 before writer sync: %q ok=%v\n", deletedValue, deletedOK)
}

func printProfile(w io.Writer, profile pagebtree.MDBKernelProfile) {
	fmt.Fprintln(w, "profile:")
	fmt.Fprintf(w, "  storage: %s\n", profile.Storage)
	fmt.Fprintf(w, "  page size: %d\n", profile.PageSize)
	fmt.Fprintf(w, "  max mapped pages: %d\n", profile.MaxMappedPages)
	fmt.Fprintf(w, "  slotted pages: %t\n", profile.SlottedPages)
	fmt.Fprintf(w, "  copy-on-write: %t\n", profile.CopyOnWrite)
	fmt.Fprintf(w, "  reader table: %t\n", profile.ReaderTable)
	fmt.Fprintf(w, "  reader-pinned recycling: %t\n", profile.ReaderPinnedRecycling)
	fmt.Fprintf(w, "  persisted reclaim records: %t\n", profile.PersistedReclaimRecords)
	fmt.Fprintf(w, "  byte-aware split points: %t\n", profile.ByteAwareSplitPoints)
	fmt.Fprintf(w, "  byte-aware delete redistribution: %t\n", profile.ByteAwareDeleteRedistribution)
	fmt.Fprintf(w, "  byte-fit delete merges: %t\n", profile.ByteFitDeleteMerges)
	fmt.Fprintf(w, "  configurable repair fill: %t\n", profile.ConfigurableRepairFill)
	fmt.Fprintf(w, "  min repair page fill: %d%%\n", profile.MinRepairPageFillPercent)
	fmt.Fprintf(w, "  kernel page cache: %t\n", profile.KernelPageCache)
	fmt.Fprintf(w, "  raw heap page cache: %t\n", profile.RawHeapPageCache)
	fmt.Fprintf(w, "  derived routing cache: %t\n", profile.DerivedBranchRoutingCache)
	fmt.Fprintf(w, "  routing cache capacity: %d\n", profile.DerivedBranchRoutingCacheCapacity)
	fmt.Fprintf(w, "  routing cache entries: %d\n", profile.DerivedBranchRoutingCacheEntries)
	fmt.Fprintf(w, "  routing cache hits: %d\n", profile.DerivedBranchRoutingCacheHits)
	fmt.Fprintf(w, "  routing cache misses: %d\n", profile.DerivedBranchRoutingCacheMisses)
}
