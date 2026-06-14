package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

const defaultTransactions = 12

type txWorkloadReport struct {
	Path                        string                          `json:"path,omitempty"`
	PathRedacted                bool                            `json:"path_redacted,omitempty"`
	TracePath                   string                          `json:"trace_path,omitempty"`
	TracePathRedacted           bool                            `json:"trace_path_redacted,omitempty"`
	Transactions                int                             `json:"transactions"`
	DeleteEvery                 int                             `json:"delete_every,omitempty"`
	Committed                   int                             `json:"committed"`
	Conflicted                  int                             `json:"conflicted"`
	DeletedCommittedKeys        int                             `json:"deleted_committed_keys,omitempty"`
	ReopenedCommittedKeys       int                             `json:"reopened_committed_keys"`
	ReopenedDeletedKeys         int                             `json:"reopened_deleted_keys"`
	ReopenedConflictedKeys      int                             `json:"reopened_conflicted_keys"`
	ReopenedOutsideKeys         int                             `json:"reopened_outside_keys"`
	Readers                     int                             `json:"readers,omitempty"`
	ActiveReadersObserved       int                             `json:"active_readers_observed,omitempty"`
	ReaderPinnedRetiredPages    int                             `json:"reader_pinned_retired_pages,omitempty"`
	ReaderPinnedFreePages       int                             `json:"reader_pinned_free_pages,omitempty"`
	ReaderPinnedReclaimPressure *pagebtree.ReclaimPressureStats `json:"reader_pinned_reclaim_pressure,omitempty"`
	FinalStats                  pagebtree.Stats                 `json:"final_stats"`
	TransactionConflictKind     string                          `json:"transaction_conflict_kind"`
}

type txWorkloadOptions struct {
	path         string
	tracePath    string
	redactPath   bool
	transactions int
	deleteEvery  int
	readers      int
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	options, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "mmap tx workload: %v\n", err)
		printUsage(stderr)
		return 2
	}

	var traceFile *os.File
	var traceExporter *pagebtree.MmapTraceJSONLExporter
	var traceHook pagebtree.MmapTraceHook
	if options.tracePath != "" {
		traceFile, err = os.Create(options.tracePath)
		if err != nil {
			fmt.Fprintf(stderr, "mmap tx workload: trace: %v\n", err)
			return 1
		}
		defer traceFile.Close()
		traceExporter = pagebtree.NewMmapTraceJSONLExporter(traceFile)
		traceHook = traceExporter.Hook()
	}

	report, err := runWorkload(options, traceHook)
	if err != nil {
		fmt.Fprintf(stderr, "mmap tx workload: %v\n", err)
		return 1
	}
	if options.redactPath {
		report.PathRedacted = true
		if options.tracePath != "" {
			report.TracePathRedacted = true
		}
	} else {
		report.TracePath = options.tracePath
	}
	if traceExporter != nil {
		if err := traceExporter.Err(); err != nil {
			fmt.Fprintf(stderr, "mmap tx workload: trace: %v\n", err)
			return 1
		}
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "mmap tx workload: encode report: %v\n", err)
		return 1
	}
	return 0
}

func parseArgs(args []string) (txWorkloadOptions, error) {
	options := txWorkloadOptions{transactions: defaultTransactions}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--transactions":
			value, ok := nextArg(args, &i)
			if !ok {
				return txWorkloadOptions{}, fmt.Errorf("--transactions expects a positive integer")
			}
			transactions, err := parsePositiveInt(value)
			if err != nil {
				return txWorkloadOptions{}, fmt.Errorf("--transactions expects a positive integer: %w", err)
			}
			options.transactions = transactions
		case strings.HasPrefix(arg, "--transactions="):
			transactions, err := parsePositiveInt(strings.TrimPrefix(arg, "--transactions="))
			if err != nil {
				return txWorkloadOptions{}, fmt.Errorf("--transactions expects a positive integer: %w", err)
			}
			options.transactions = transactions
		case arg == "--delete-every":
			value, ok := nextArg(args, &i)
			if !ok {
				return txWorkloadOptions{}, fmt.Errorf("--delete-every expects a positive integer")
			}
			deleteEvery, err := parsePositiveInt(value)
			if err != nil {
				return txWorkloadOptions{}, fmt.Errorf("--delete-every expects a positive integer: %w", err)
			}
			options.deleteEvery = deleteEvery
		case strings.HasPrefix(arg, "--delete-every="):
			deleteEvery, err := parsePositiveInt(strings.TrimPrefix(arg, "--delete-every="))
			if err != nil {
				return txWorkloadOptions{}, fmt.Errorf("--delete-every expects a positive integer: %w", err)
			}
			options.deleteEvery = deleteEvery
		case arg == "--readers":
			value, ok := nextArg(args, &i)
			if !ok {
				return txWorkloadOptions{}, fmt.Errorf("--readers expects a positive integer")
			}
			readers, err := parsePositiveInt(value)
			if err != nil {
				return txWorkloadOptions{}, fmt.Errorf("--readers expects a positive integer: %w", err)
			}
			options.readers = readers
		case strings.HasPrefix(arg, "--readers="):
			readers, err := parsePositiveInt(strings.TrimPrefix(arg, "--readers="))
			if err != nil {
				return txWorkloadOptions{}, fmt.Errorf("--readers expects a positive integer: %w", err)
			}
			options.readers = readers
		case arg == "--trace":
			value, ok := nextArg(args, &i)
			if !ok || value == "" {
				return txWorkloadOptions{}, fmt.Errorf("--trace expects a JSONL path")
			}
			options.tracePath = value
		case strings.HasPrefix(arg, "--trace="):
			options.tracePath = strings.TrimPrefix(arg, "--trace=")
			if options.tracePath == "" {
				return txWorkloadOptions{}, fmt.Errorf("--trace expects a JSONL path")
			}
		case arg == "--redact-path":
			options.redactPath = true
		default:
			if arg == "" || strings.HasPrefix(arg, "-") {
				return txWorkloadOptions{}, fmt.Errorf("unknown argument %q", arg)
			}
			if options.path != "" {
				return txWorkloadOptions{}, fmt.Errorf("expected one DB path")
			}
			options.path = arg
		}
	}
	if options.path == "" {
		return txWorkloadOptions{}, fmt.Errorf("expected one DB path")
	}
	return options, nil
}

func nextArg(args []string, index *int) (string, bool) {
	*index++
	if *index >= len(args) {
		return "", false
	}
	return args[*index], true
}

func parsePositiveInt(text string) (int, error) {
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("got %d", value)
	}
	return value, nil
}

func printUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "usage: mmaptxworkload [--transactions N] [--delete-every N] [--readers N] [--trace TRACE.jsonl] [--redact-path] DB.db")
}

func runWorkload(options txWorkloadOptions, traceHook pagebtree.MmapTraceHook) (txWorkloadReport, error) {
	if err := refuseExistingArtifacts(options.path); err != nil {
		return txWorkloadReport{}, err
	}

	tree, err := pagebtree.OpenMmap(options.path, pagebtree.MmapOptions{
		Degree:    2,
		MaxPages:  maxPagesForTransactions(options.transactions),
		TraceHook: traceHook,
	})
	if err != nil {
		return txWorkloadReport{}, err
	}
	defer tree.Close()

	tree.Put("seed", []byte("seed"))
	if err := tree.Sync(); err != nil {
		return txWorkloadReport{}, err
	}

	report := txWorkloadReport{
		Transactions:            options.transactions,
		DeleteEvery:             options.deleteEvery,
		Readers:                 options.readers,
		TransactionConflictKind: string(pagebtree.MmapTraceTxConflict),
	}
	if !options.redactPath {
		report.Path = options.path
	}

	readers, err := openReadOnlyReaders(options.path, options.readers)
	if err != nil {
		return txWorkloadReport{}, err
	}
	defer func() {
		_ = closeReadOnlyReaders(readers)
	}()
	if options.readers > 0 {
		readerStats, err := tree.MmapReaderStats()
		if err != nil {
			return txWorkloadReport{}, err
		}
		report.ActiveReadersObserved = readerStats.ActiveSlots
	}

	liveCommittedKeys := []string{}
	deletedCommittedKeys := map[string]bool{}
	for i := 0; i < options.transactions; i++ {
		tx := tree.BeginReadWrite()
		key := txKey(i)
		tx.Put(key, []byte(fmt.Sprintf("tx-value-%04d", i)))
		if i%2 == 0 {
			tree.Put(outsideKey(i), []byte(fmt.Sprintf("outside-value-%04d", i)))
			if err := tree.Sync(); err != nil {
				return txWorkloadReport{}, err
			}
			result, err := tx.CommitSyncDetailed()
			if !errors.Is(err, pagebtree.ErrTxConflict) {
				return txWorkloadReport{}, fmt.Errorf("transaction %d conflict commit error = %v, want ErrTxConflict", i, err)
			}
			if result.Changed {
				return txWorkloadReport{}, fmt.Errorf("transaction %d conflicted result changed", i)
			}
			report.Conflicted++
			continue
		}
		var deletedKey string
		if options.deleteEvery > 0 && (report.Committed+1)%options.deleteEvery == 0 && len(liveCommittedKeys) > 0 {
			deletedKey = liveCommittedKeys[len(liveCommittedKeys)-1]
			tx.Delete(deletedKey)
		}
		result, err := tx.CommitSyncDetailed()
		if err != nil {
			return txWorkloadReport{}, fmt.Errorf("transaction %d commit-sync: %w", i, err)
		}
		if !result.Changed {
			return txWorkloadReport{}, fmt.Errorf("transaction %d did not change tree", i)
		}
		report.Committed++
		if deletedKey != "" {
			liveCommittedKeys = liveCommittedKeys[:len(liveCommittedKeys)-1]
			deletedCommittedKeys[deletedKey] = true
			report.DeletedCommittedKeys++
		}
		liveCommittedKeys = append(liveCommittedKeys, key)
	}

	if err := tree.Check(); err != nil {
		return txWorkloadReport{}, err
	}

	if options.readers > 0 {
		pinned := tree.Stats()
		report.ReaderPinnedRetiredPages = pinned.RetiredPages
		report.ReaderPinnedFreePages = pinned.FreePages
		pressure := pinned.ReclaimPressure
		report.ReaderPinnedReclaimPressure = &pressure
		if err := closeReadOnlyReaders(readers); err != nil {
			return txWorkloadReport{}, err
		}
		readers = nil
		tree.Put("reader-release-reclaim-trigger", []byte("done"))
		if err := tree.Sync(); err != nil {
			return txWorkloadReport{}, err
		}
		if err := tree.Check(); err != nil {
			return txWorkloadReport{}, err
		}
	}
	report.FinalStats = tree.Stats()
	if err := tree.Close(); err != nil {
		return txWorkloadReport{}, err
	}
	tree = nil

	reopened, err := pagebtree.OpenMmap(options.path, pagebtree.MmapOptions{})
	if err != nil {
		return txWorkloadReport{}, err
	}
	defer reopened.Close()
	for i := 0; i < options.transactions; i++ {
		key := txKey(i)
		if _, ok := reopened.Get(key); ok {
			if i%2 == 0 {
				report.ReopenedConflictedKeys++
			} else if deletedCommittedKeys[key] {
				report.ReopenedDeletedKeys++
			} else {
				report.ReopenedCommittedKeys++
			}
		}
		if _, ok := reopened.Get(outsideKey(i)); ok {
			report.ReopenedOutsideKeys++
		}
	}
	return report, nil
}

func openReadOnlyReaders(path string, count int) ([]*pagebtree.Tree, error) {
	readers := make([]*pagebtree.Tree, 0, count)
	for i := 0; i < count; i++ {
		reader, err := pagebtree.OpenMmapReadOnly(path)
		if err != nil {
			_ = closeReadOnlyReaders(readers)
			return nil, err
		}
		if value, ok := reader.Get("seed"); !ok || len(value) == 0 {
			_ = reader.Close()
			_ = closeReadOnlyReaders(readers)
			return nil, fmt.Errorf("reader %d could not observe seed key", i)
		}
		readers = append(readers, reader)
	}
	return readers, nil
}

func closeReadOnlyReaders(readers []*pagebtree.Tree) error {
	var closeErr error
	for _, reader := range readers {
		if reader == nil {
			continue
		}
		if err := reader.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func refuseExistingArtifacts(path string) error {
	for _, candidate := range []string{path, path + ".readers", path + ".writer"} {
		if _, err := os.Stat(candidate); err == nil {
			return fmt.Errorf("refusing existing database artifact %s", candidate)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func maxPagesForTransactions(transactions int) int {
	pages := 64 + transactions*8
	if pages < 128 {
		return 128
	}
	return pages
}

func txKey(index int) string {
	return fmt.Sprintf("tx-key-%04d", index)
}

func outsideKey(index int) string {
	return fmt.Sprintf("outside-%04d", index)
}
