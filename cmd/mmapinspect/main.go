package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

type inspectReport struct {
	Valid             bool                        `json:"valid"`
	Error             string                      `json:"error,omitempty"`
	Stats             pagebtree.Stats             `json:"stats"`
	KeyOrder          pagebtree.KeyOrder          `json:"key_order"`
	KeyOrderName      string                      `json:"key_order_name"`
	KeyComparator     pagebtree.KeyComparatorKind `json:"key_comparator"`
	KeyComparatorName string                      `json:"key_comparator_name"`
	ReachablePageIDs  []pagebtree.PageID          `json:"reachable_page_ids"`
	FreePageIDs       []pagebtree.PageID          `json:"free_page_ids"`
	RetiredPageIDs    []pagebtree.PageID          `json:"retired_page_ids"`
	LeafLinksChecked  bool                        `json:"leaf_links_checked"`
	LeafLinksSkipped  bool                        `json:"leaf_links_skipped"`
	ReaderStats       *pagebtree.MmapReaderStats  `json:"reader_stats,omitempty"`
	CacheStats        *pagebtree.MmapCacheStats   `json:"cache_stats,omitempty"`
}

type inspectOptions struct {
	path    string
	readers bool
	cache   bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	options, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "mmap inspect: %v\n", err)
		printUsage(stderr)
		return 2
	}

	tree, err := pagebtree.OpenMmapReadOnly(options.path)
	if err != nil {
		fmt.Fprintf(stderr, "mmap inspect: %v\n", err)
		return 1
	}
	defer tree.Close()

	report := inspectFromAudit(tree.Audit(), tree.MDBKernelProfile())
	if options.readers {
		stats, err := tree.MmapReaderStats()
		if err != nil {
			fmt.Fprintf(stderr, "mmap inspect: reader stats: %v\n", err)
			return 1
		}
		report.ReaderStats = &stats
	}
	if options.cache {
		stats, err := tree.MmapCacheStats()
		if err != nil {
			fmt.Fprintf(stderr, "mmap inspect: cache stats: %v\n", err)
			return 1
		}
		report.CacheStats = &stats
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "mmap inspect: encode report: %v\n", err)
		return 1
	}
	if !report.Valid {
		return 1
	}
	return 0
}

func parseArgs(args []string) (inspectOptions, error) {
	var options inspectOptions
	for _, arg := range args {
		switch arg {
		case "--readers":
			options.readers = true
		case "--cache":
			options.cache = true
		default:
			if len(arg) > 0 && arg[0] == '-' {
				return inspectOptions{}, fmt.Errorf("unknown option %q", arg)
			}
			if options.path != "" {
				return inspectOptions{}, fmt.Errorf("expected one database path")
			}
			options.path = arg
		}
	}
	if options.path == "" {
		return inspectOptions{}, fmt.Errorf("expected one database path")
	}
	return options, nil
}

func printUsage(stderr io.Writer) {
	fmt.Fprintf(stderr, "usage: mmapinspect [--readers] [--cache] DB.db\n")
}

func inspectFromAudit(audit pagebtree.AuditReport, profile pagebtree.MDBKernelProfile) inspectReport {
	report := inspectReport{
		Valid:             audit.Valid(),
		Stats:             audit.Stats,
		KeyOrder:          profile.KeyOrder,
		KeyOrderName:      profile.KeyOrder.String(),
		KeyComparator:     profile.KeyComparator,
		KeyComparatorName: profile.KeyComparator.String(),
		ReachablePageIDs:  audit.ReachablePageIDs,
		FreePageIDs:       audit.FreePageIDs,
		RetiredPageIDs:    audit.RetiredPageIDs,
		LeafLinksChecked:  audit.LeafLinksChecked,
		LeafLinksSkipped:  audit.LeafLinksSkipped,
	}
	if audit.Error != nil {
		report.Error = audit.Error.Error()
	}
	return report
}
