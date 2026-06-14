package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

// Trace next_page includes the two checked metadata pages; Stats.AllocatedPages
// counts only database pages after them.
const inspectMmapMetaPages = 2

type inspectReport struct {
	Valid             bool                               `json:"valid"`
	Error             string                             `json:"error,omitempty"`
	Stats             pagebtree.Stats                    `json:"stats"`
	KeyOrder          pagebtree.KeyOrder                 `json:"key_order"`
	KeyOrderName      string                             `json:"key_order_name"`
	KeyComparator     pagebtree.KeyComparatorKind        `json:"key_comparator"`
	KeyComparatorName string                             `json:"key_comparator_name"`
	ReachablePageIDs  []pagebtree.PageID                 `json:"reachable_page_ids"`
	FreePageIDs       []pagebtree.PageID                 `json:"free_page_ids"`
	RetiredPageIDs    []pagebtree.PageID                 `json:"retired_page_ids"`
	LeafLinksChecked  bool                               `json:"leaf_links_checked"`
	LeafLinksSkipped  bool                               `json:"leaf_links_skipped"`
	ReaderStats       *pagebtree.MmapReaderStats         `json:"reader_stats,omitempty"`
	CacheStats        *pagebtree.MmapCacheStats          `json:"cache_stats,omitempty"`
	SpaceStats        *pagebtree.MmapSpaceStats          `json:"space_stats,omitempty"`
	HolePunchProfile  *pagebtree.MmapHolePunchCapability `json:"hole_punch_profile,omitempty"`
	KeySample         *inspectKeySample                  `json:"key_sample,omitempty"`
	PageSummaries     []pagebtree.PageSummary            `json:"page_summaries,omitempty"`
	TraceSummary      *inspectTraceSummary               `json:"trace_summary,omitempty"`
}

type inspectKeySample struct {
	Limit     int      `json:"limit"`
	First     []string `json:"first"`
	Last      []string `json:"last"`
	Truncated bool     `json:"truncated"`
}

type inspectTraceSummary struct {
	Path                       string           `json:"path"`
	Events                     int              `json:"events"`
	KindCounts                 map[string]int   `json:"kind_counts"`
	DirtyDataRanges            int              `json:"dirty_data_ranges"`
	DirtyDataPages             int              `json:"dirty_data_pages"`
	MaxDirtyRangePages         int              `json:"max_dirty_range_pages"`
	MaxDirtyRangeDurationNanos int64            `json:"max_dirty_range_duration_nanos"`
	PunchRanges                int              `json:"punch_ranges"`
	PunchedPages               int              `json:"punched_pages"`
	PunchedBytes               int64            `json:"punched_bytes"`
	SkippedRecoverablePages    int              `json:"skipped_recoverable_pages"`
	MaxPunchRangePages         int              `json:"max_punch_range_pages"`
	PunchFailures              int              `json:"punch_failures"`
	LastRevision               uint64           `json:"last_revision,omitempty"`
	LastRoot                   pagebtree.PageID `json:"last_root,omitempty"`
	LastNextPage               pagebtree.PageID `json:"last_next_page,omitempty"`
	LastMaxPages               int              `json:"last_max_pages,omitempty"`
	MatchesCurrentRevision     bool             `json:"matches_current_revision"`
	MatchesCurrentRoot         bool             `json:"matches_current_root"`
	MatchesCurrentNextPage     bool             `json:"matches_current_next_page"`
	HasFailures                bool             `json:"has_failures"`
	FailureReasons             []string         `json:"failure_reasons,omitempty"`
}

type inspectOptions struct {
	path           string
	readers        bool
	cache          bool
	space          bool
	pages          bool
	keySampleLimit int
	tracePath      string
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
	if options.space {
		stats, err := tree.MmapSpaceStats()
		if err != nil {
			fmt.Fprintf(stderr, "mmap inspect: space stats: %v\n", err)
			return 1
		}
		report.SpaceStats = &stats
		profile := pagebtree.MmapHolePunchProfile()
		report.HolePunchProfile = &profile
	}
	if options.keySampleLimit > 0 {
		sample := inspectKeys(tree, options.keySampleLimit)
		report.KeySample = &sample
	}
	if options.tracePath != "" {
		summary, err := inspectTrace(options.tracePath, report.Stats)
		if err != nil {
			fmt.Fprintf(stderr, "mmap inspect: trace: %v\n", err)
			return 1
		}
		report.TraceSummary = &summary
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
		case "--space":
			options.space = true
		case "--pages":
			options.pages = true
		case "--trace":
			i++
			if i >= len(args) {
				return inspectOptions{}, fmt.Errorf("--trace expects a JSONL path")
			}
			options.tracePath = args[i]
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
			if strings.HasPrefix(arg, "--trace=") {
				options.tracePath = strings.TrimPrefix(arg, "--trace=")
				if options.tracePath == "" {
					return inspectOptions{}, fmt.Errorf("--trace expects a JSONL path")
				}
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
	fmt.Fprintf(stderr, "usage: mmapinspect [--readers] [--cache] [--space] [--pages] [--keys N] [--trace TRACE.jsonl] DB.db\n")
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

func inspectTrace(path string, stats pagebtree.Stats) (inspectTraceSummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return inspectTraceSummary{}, err
	}
	defer file.Close()

	summary := inspectTraceSummary{
		Path:       path,
		KindCounts: make(map[string]int),
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event pagebtree.MmapTraceEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return inspectTraceSummary{}, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		summary.add(event)
	}
	if err := scanner.Err(); err != nil {
		return inspectTraceSummary{}, err
	}
	summary.MatchesCurrentRevision = summary.LastRevision == stats.Revision
	summary.MatchesCurrentRoot = summary.LastRoot == stats.Root
	currentNextPage := pagebtree.PageID(stats.AllocatedPages + inspectMmapMetaPages)
	summary.MatchesCurrentNextPage = summary.LastNextPage == currentNextPage
	return summary, nil
}

func (s *inspectTraceSummary) add(event pagebtree.MmapTraceEvent) {
	s.Events++
	s.KindCounts[string(event.Kind)]++
	if event.Revision != 0 {
		s.LastRevision = event.Revision
	}
	if event.Root != 0 {
		s.LastRoot = event.Root
	}
	if event.NextPage != 0 {
		s.LastNextPage = event.NextPage
	}
	if event.NewNextPage != 0 {
		s.LastNextPage = event.NewNextPage
	}
	if event.MaxPages != 0 {
		s.LastMaxPages = event.MaxPages
	}
	if event.NewMaxPages != 0 {
		s.LastMaxPages = event.NewMaxPages
	}
	if event.Kind == pagebtree.MmapTraceSyncDataRange {
		pages := int(event.EndPage - event.StartPage)
		if event.EndPage > event.StartPage {
			s.DirtyDataRanges++
			s.DirtyDataPages += pages
			if pages > s.MaxDirtyRangePages {
				s.MaxDirtyRangePages = pages
			}
		}
		if event.DurationNanos > s.MaxDirtyRangeDurationNanos {
			s.MaxDirtyRangeDurationNanos = event.DurationNanos
		}
	}
	if event.SkippedRecoverablePages != 0 {
		s.SkippedRecoverablePages = event.SkippedRecoverablePages
	}
	switch event.Kind {
	case pagebtree.MmapTracePunchRange:
		pages := event.PunchedPages
		if pages == 0 && event.EndPage > event.StartPage {
			pages = int(event.EndPage - event.StartPage)
		}
		bytes := event.PunchedBytes
		if bytes == 0 && pages > 0 {
			bytes = int64(pages * pagebtree.PageSize)
		}
		if pages > 0 {
			s.PunchRanges++
			s.PunchedPages += pages
			s.PunchedBytes += bytes
			if pages > s.MaxPunchRangePages {
				s.MaxPunchRangePages = pages
			}
		}
	case pagebtree.MmapTracePunchEnd:
		if s.PunchRanges == 0 {
			s.PunchRanges = event.PunchRanges
			s.PunchedPages = event.PunchedPages
			s.PunchedBytes = event.PunchedBytes
			if event.PunchedPages > s.MaxPunchRangePages {
				s.MaxPunchRangePages = event.PunchedPages
			}
		}
	case pagebtree.MmapTracePunchFailed:
		s.PunchFailures++
		if s.PunchRanges == 0 {
			s.PunchRanges = event.PunchRanges
			s.PunchedPages = event.PunchedPages
			s.PunchedBytes = event.PunchedBytes
			if event.PunchedPages > s.MaxPunchRangePages {
				s.MaxPunchRangePages = event.PunchedPages
			}
		}
	}
	if strings.Contains(string(event.Kind), "failed") || event.Kind == pagebtree.MmapTraceRecoveryCandidateRejected {
		s.HasFailures = true
		if event.Reason != "" {
			s.FailureReasons = append(s.FailureReasons, event.Reason)
		}
	}
}
