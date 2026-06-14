package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

const defaultTimelineLimit = 64

type traceInput struct {
	name   string
	reader io.Reader
}

type traceSummary struct {
	inputs                     []string
	events                     int
	kindCounts                 map[string]int
	timeline                   []traceTimelineRow
	timelineLimit              int
	timelineTruncated          bool
	dirtyDataRanges            int
	dirtyDataPages             int
	maxDirtyRangePages         int
	maxDirtyRangeDurationNanos int64
	punchRanges                int
	punchedPages               int
	punchedBytes               int64
	maxPunchRangePages         int
	failures                   int
	failureReasons             []string
}

type traceTimelineRow struct {
	index         int
	kind          string
	revision      uint64
	root          pagebtree.PageID
	nextPage      pagebtree.PageID
	pages         int
	durationNanos int64
	reason        string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	options, err := parseOptions(args)
	if err != nil {
		fmt.Fprintf(stderr, "mmap trace summary: %v\n", err)
		printUsage(stderr)
		return 2
	}
	inputs, closers, err := openTraceInputs(options.paths, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "mmap trace summary: %v\n", err)
		return 1
	}
	for _, closer := range closers {
		defer closer.Close()
	}

	summary, err := summarizeTraceInputs(inputs, options.limit)
	if err != nil {
		fmt.Fprintf(stderr, "mmap trace summary: %v\n", err)
		return 1
	}
	writeMarkdown(summary, stdout)
	return 0
}

type traceOptions struct {
	limit int
	paths []string
}

func parseOptions(args []string) (traceOptions, error) {
	options := traceOptions{limit: defaultTimelineLimit}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--limit":
			i++
			if i >= len(args) {
				return traceOptions{}, fmt.Errorf("--limit expects a non-negative integer")
			}
			limit, err := parseLimit(args[i])
			if err != nil {
				return traceOptions{}, fmt.Errorf("--limit expects a non-negative integer: %w", err)
			}
			options.limit = limit
		case strings.HasPrefix(arg, "--limit="):
			limit, err := parseLimit(strings.TrimPrefix(arg, "--limit="))
			if err != nil {
				return traceOptions{}, fmt.Errorf("--limit expects a non-negative integer: %w", err)
			}
			options.limit = limit
		case strings.HasPrefix(arg, "-"):
			return traceOptions{}, fmt.Errorf("unknown option %q", arg)
		default:
			options.paths = append(options.paths, arg)
		}
	}
	return options, nil
}

func parseLimit(raw string) (int, error) {
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if limit < 0 {
		return 0, fmt.Errorf("got %d", limit)
	}
	return limit, nil
}

func printUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "usage: mmaptracesummary [--limit N] [TRACE.jsonl ...]")
}

func openTraceInputs(paths []string, stdin io.Reader) ([]traceInput, []io.Closer, error) {
	if len(paths) == 0 {
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		return []traceInput{{name: "stdin", reader: stdin}}, nil, nil
	}
	inputs := make([]traceInput, 0, len(paths))
	closers := make([]io.Closer, 0, len(paths))
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			for _, closer := range closers {
				_ = closer.Close()
			}
			return nil, nil, err
		}
		inputs = append(inputs, traceInput{name: filepath.Base(path), reader: file})
		closers = append(closers, file)
	}
	return inputs, closers, nil
}

func summarizeTraceInputs(inputs []traceInput, limit int) (traceSummary, error) {
	summary := traceSummary{
		kindCounts:    map[string]int{},
		timelineLimit: limit,
	}
	for _, input := range inputs {
		summary.inputs = append(summary.inputs, input.name)
		if err := summary.addInput(input); err != nil {
			return traceSummary{}, fmt.Errorf("%s: %w", input.name, err)
		}
	}
	if summary.events == 0 {
		return traceSummary{}, fmt.Errorf("no trace events found")
	}
	return summary, nil
}

func (s *traceSummary) addInput(input traceInput) error {
	scanner := bufio.NewScanner(input.reader)
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
			return fmt.Errorf("line %d: %w", lineNumber, err)
		}
		s.addEvent(event)
	}
	return scanner.Err()
}

func (s *traceSummary) addEvent(event pagebtree.MmapTraceEvent) {
	s.events++
	s.kindCounts[string(event.Kind)]++
	s.addTimeline(event)
	if event.Kind == pagebtree.MmapTraceSyncDataRange {
		pages := eventPages(event)
		if pages > 0 {
			s.dirtyDataRanges++
			s.dirtyDataPages += pages
			if pages > s.maxDirtyRangePages {
				s.maxDirtyRangePages = pages
			}
		}
		if event.DurationNanos > s.maxDirtyRangeDurationNanos {
			s.maxDirtyRangeDurationNanos = event.DurationNanos
		}
	}
	switch event.Kind {
	case pagebtree.MmapTracePunchRange:
		s.addPunchRange(event)
	case pagebtree.MmapTracePunchEnd:
		if s.punchRanges == 0 {
			s.punchRanges = event.PunchRanges
			s.punchedPages = event.PunchedPages
			s.punchedBytes = event.PunchedBytes
			s.maxPunchRangePages = event.PunchedPages
		}
	case pagebtree.MmapTracePunchFailed:
		if s.punchRanges == 0 {
			s.punchRanges = event.PunchRanges
			s.punchedPages = event.PunchedPages
			s.punchedBytes = event.PunchedBytes
			s.maxPunchRangePages = event.PunchedPages
		}
	}
	if strings.Contains(string(event.Kind), "failed") || event.Kind == pagebtree.MmapTraceRecoveryCandidateRejected {
		s.failures++
		if event.Reason != "" {
			s.failureReasons = append(s.failureReasons, event.Reason)
		}
	}
}

func (s *traceSummary) addPunchRange(event pagebtree.MmapTraceEvent) {
	pages := event.PunchedPages
	if pages == 0 {
		pages = eventPages(event)
	}
	bytes := event.PunchedBytes
	if bytes == 0 && pages > 0 {
		bytes = int64(pages * pagebtree.PageSize)
	}
	if pages <= 0 {
		return
	}
	s.punchRanges++
	s.punchedPages += pages
	s.punchedBytes += bytes
	if pages > s.maxPunchRangePages {
		s.maxPunchRangePages = pages
	}
}

func (s *traceSummary) addTimeline(event pagebtree.MmapTraceEvent) {
	if len(s.timeline) >= s.timelineLimit {
		s.timelineTruncated = true
		return
	}
	s.timeline = append(s.timeline, traceTimelineRow{
		index:         s.events,
		kind:          string(event.Kind),
		revision:      event.Revision,
		root:          event.Root,
		nextPage:      effectiveNextPage(event),
		pages:         eventPages(event),
		durationNanos: event.DurationNanos,
		reason:        event.Reason,
	})
}

func effectiveNextPage(event pagebtree.MmapTraceEvent) pagebtree.PageID {
	if event.NewNextPage != 0 {
		return event.NewNextPage
	}
	return event.NextPage
}

func eventPages(event pagebtree.MmapTraceEvent) int {
	if event.EndPage > event.StartPage {
		return int(event.EndPage - event.StartPage)
	}
	if event.PunchedPages > 0 {
		return event.PunchedPages
	}
	return 0
}

func writeMarkdown(summary traceSummary, writer io.Writer) {
	fmt.Fprintln(writer, "# mmap Trace Summary")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "## Overview")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "| Metric | Value |")
	fmt.Fprintln(writer, "| --- | --- |")
	fmt.Fprintf(writer, "| Inputs | %s |\n", strings.Join(summary.inputs, ", "))
	fmt.Fprintf(writer, "| Events | %d |\n", summary.events)
	fmt.Fprintf(writer, "| Timeline limit | %d |\n", summary.timelineLimit)
	fmt.Fprintf(writer, "| Timeline truncated | %t |\n", summary.timelineTruncated)
	fmt.Fprintf(writer, "| Dirty data ranges | %d |\n", summary.dirtyDataRanges)
	fmt.Fprintf(writer, "| Dirty data pages | %d |\n", summary.dirtyDataPages)
	fmt.Fprintf(writer, "| Max dirty range pages | %d |\n", summary.maxDirtyRangePages)
	fmt.Fprintf(writer, "| Max dirty range duration ns | %d |\n", summary.maxDirtyRangeDurationNanos)
	fmt.Fprintf(writer, "| Punch ranges | %d |\n", summary.punchRanges)
	fmt.Fprintf(writer, "| Punched pages | %d |\n", summary.punchedPages)
	fmt.Fprintf(writer, "| Punched bytes | %d |\n", summary.punchedBytes)
	fmt.Fprintf(writer, "| Max punch range pages | %d |\n", summary.maxPunchRangePages)
	fmt.Fprintf(writer, "| Failures | %d |\n", summary.failures)
	if len(summary.failureReasons) > 0 {
		fmt.Fprintf(writer, "| Failure reasons | %s |\n", escapeCell(strings.Join(summary.failureReasons, "; ")))
	}
	fmt.Fprintln(writer)
	writeKindCounts(summary, writer)
	writeTimeline(summary, writer)
}

func writeKindCounts(summary traceSummary, writer io.Writer) {
	fmt.Fprintln(writer, "## Event Kinds")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "| Kind | Count |")
	fmt.Fprintln(writer, "| --- | ---: |")
	kinds := make([]string, 0, len(summary.kindCounts))
	for kind := range summary.kindCounts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		fmt.Fprintf(writer, "| %s | %d |\n", escapeCell(kind), summary.kindCounts[kind])
	}
	fmt.Fprintln(writer)
}

func writeTimeline(summary traceSummary, writer io.Writer) {
	fmt.Fprintln(writer, "## Timeline")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "| # | Kind | Revision | Root | Next page | Pages | Duration ns | Reason |")
	fmt.Fprintln(writer, "| ---: | --- | ---: | ---: | ---: | ---: | ---: | --- |")
	for _, row := range summary.timeline {
		fmt.Fprintf(
			writer,
			"| %d | %s | %d | %d | %d | %d | %d | %s |\n",
			row.index,
			escapeCell(row.kind),
			row.revision,
			row.root,
			row.nextPage,
			row.pages,
			row.durationNanos,
			escapeCell(row.reason),
		)
	}
}

func escapeCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.ReplaceAll(value, "|", "\\|")
}
