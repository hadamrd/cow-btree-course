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
	Path                    string          `json:"path"`
	TracePath               string          `json:"trace_path,omitempty"`
	Transactions            int             `json:"transactions"`
	Committed               int             `json:"committed"`
	Conflicted              int             `json:"conflicted"`
	ReopenedCommittedKeys   int             `json:"reopened_committed_keys"`
	ReopenedConflictedKeys  int             `json:"reopened_conflicted_keys"`
	ReopenedOutsideKeys     int             `json:"reopened_outside_keys"`
	FinalStats              pagebtree.Stats `json:"final_stats"`
	TransactionConflictKind string          `json:"transaction_conflict_kind"`
}

type txWorkloadOptions struct {
	path         string
	tracePath    string
	transactions int
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
	report.TracePath = options.tracePath
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
	fmt.Fprintln(stderr, "usage: mmaptxworkload [--transactions N] [--trace TRACE.jsonl] DB.db")
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
		Path:                    options.path,
		Transactions:            options.transactions,
		TransactionConflictKind: string(pagebtree.MmapTraceTxConflict),
	}

	for i := 0; i < options.transactions; i++ {
		tx := tree.BeginReadWrite()
		tx.Put(txKey(i), []byte(fmt.Sprintf("tx-value-%04d", i)))
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
		result, err := tx.CommitSyncDetailed()
		if err != nil {
			return txWorkloadReport{}, fmt.Errorf("transaction %d commit-sync: %w", i, err)
		}
		if !result.Changed {
			return txWorkloadReport{}, fmt.Errorf("transaction %d did not change tree", i)
		}
		report.Committed++
	}

	if err := tree.Check(); err != nil {
		return txWorkloadReport{}, err
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
		if _, ok := reopened.Get(txKey(i)); ok {
			if i%2 == 0 {
				report.ReopenedConflictedKeys++
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
