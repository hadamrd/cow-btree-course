package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

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
	KeySample         *inspectKeySample           `json:"key_sample,omitempty"`
	PageSummaries     []pagebtree.PageSummary     `json:"page_summaries,omitempty"`
}

type inspectKeySample struct {
	Limit     int      `json:"limit"`
	First     []string `json:"first"`
	Last      []string `json:"last"`
	Truncated bool     `json:"truncated"`
}

type inspectOptions struct {
	path           string
	readers        bool
	cache          bool
	pages          bool
	keySampleLimit int
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
	if !options.pages {
		report.PageSummaries = nil
	}
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
	if options.keySampleLimit > 0 {
		sample := inspectKeys(tree, options.keySampleLimit)
		report.KeySample = &sample
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
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--readers":
			options.readers = true
		case "--cache":
			options.cache = true
		case "--pages":
			options.pages = true
		case "--keys":
			i++
			if i >= len(args) {
				return inspectOptions{}, fmt.Errorf("--keys expects a positive limit")
			}
			limit, err := parsePositiveLimit(args[i])
			if err != nil {
				return inspectOptions{}, fmt.Errorf("--keys expects a positive limit: %w", err)
			}
			options.keySampleLimit = limit
		default:
			if strings.HasPrefix(arg, "--keys=") {
				limit, err := parsePositiveLimit(strings.TrimPrefix(arg, "--keys="))
				if err != nil {
					return inspectOptions{}, fmt.Errorf("--keys expects a positive limit: %w", err)
				}
				options.keySampleLimit = limit
				continue
			}
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

func parsePositiveLimit(raw string) (int, error) {
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if limit <= 0 {
		return 0, fmt.Errorf("got %d", limit)
	}
	return limit, nil
}

func printUsage(stderr io.Writer) {
	fmt.Fprintf(stderr, "usage: mmapinspect [--readers] [--cache] [--pages] [--keys N] DB.db\n")
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
		PageSummaries:     audit.PageSummaries,
		LeafLinksChecked:  audit.LeafLinksChecked,
		LeafLinksSkipped:  audit.LeafLinksSkipped,
	}
	if audit.Error != nil {
		report.Error = audit.Error.Error()
	}
	return report
}

func inspectKeys(tree *pagebtree.Tree, limit int) inspectKeySample {
	sample := inspectKeySample{
		Limit: limit,
		First: make([]string, 0, limit),
		Last:  make([]string, 0, limit),
	}
	tree.Range(func(key string, _ []byte) bool {
		if len(sample.First) < limit {
			sample.First = append(sample.First, key)
		}
		if len(sample.Last) < limit {
			sample.Last = append(sample.Last, key)
		} else {
			copy(sample.Last, sample.Last[1:])
			sample.Last[len(sample.Last)-1] = key
			sample.Truncated = true
		}
		return true
	})
	return sample
}
