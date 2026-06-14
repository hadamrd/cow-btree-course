package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

type punchReport struct {
	Path             string                            `json:"path"`
	TracePath        string                            `json:"trace_path,omitempty"`
	HolePunchProfile pagebtree.MmapHolePunchCapability `json:"hole_punch_profile"`
	BeforeSpace      pagebtree.MmapSpaceStats          `json:"before_space"`
	AfterSpace       pagebtree.MmapSpaceStats          `json:"after_space"`
	PunchStats       pagebtree.MmapHolePunchStats      `json:"punch_stats"`
	Error            string                            `json:"error,omitempty"`
}

type punchOptions struct {
	path      string
	tracePath string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	options, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "mmap punch: %v\n", err)
		printUsage(stderr)
		return 2
	}
	var traceFile *os.File
	var traceExporter *pagebtree.MmapTraceJSONLExporter
	var traceHook pagebtree.MmapTraceHook
	if options.tracePath != "" {
		traceFile, err = os.Create(options.tracePath)
		if err != nil {
			fmt.Fprintf(stderr, "mmap punch: trace: %v\n", err)
			return 1
		}
		defer traceFile.Close()
		traceExporter = pagebtree.NewMmapTraceJSONLExporter(traceFile)
		traceHook = traceExporter.Hook()
	}
	report, err := punchMmapFile(options.path, traceHook)
	if err != nil {
		fmt.Fprintf(stderr, "mmap punch: %v\n", err)
		return 1
	}
	report.TracePath = options.tracePath
	if traceExporter != nil {
		if err := traceExporter.Err(); err != nil {
			fmt.Fprintf(stderr, "mmap punch: trace: %v\n", err)
			return 1
		}
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "mmap punch: encode report: %v\n", err)
		return 1
	}
	return 0
}

func parseArgs(args []string) (punchOptions, error) {
	var options punchOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--trace" {
			i++
			if i >= len(args) || args[i] == "" {
				return punchOptions{}, fmt.Errorf("--trace expects a JSONL path")
			}
			options.tracePath = args[i]
			continue
		}
		if len(arg) > len("--trace=") && arg[:len("--trace=")] == "--trace=" {
			options.tracePath = arg[len("--trace="):]
			if options.tracePath == "" {
				return punchOptions{}, fmt.Errorf("--trace expects a JSONL path")
			}
			continue
		}
		if arg == "" || arg[0] == '-' {
			return punchOptions{}, fmt.Errorf("unknown argument %q", arg)
		}
		if options.path != "" {
			return punchOptions{}, fmt.Errorf("expected one DB path")
		}
		options.path = arg
	}
	if options.path == "" {
		return punchOptions{}, fmt.Errorf("expected one DB path")
	}
	return options, nil
}

func printUsage(stderr io.Writer) {
	fmt.Fprintf(stderr, "usage: mmappunch [--trace TRACE.jsonl] DB.db\n")
}

func punchMmapFile(path string, traceHook pagebtree.MmapTraceHook) (punchReport, error) {
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{TraceHook: traceHook})
	if err != nil {
		return punchReport{}, err
	}
	defer tree.Close()

	before, err := tree.MmapSpaceStats()
	if err != nil {
		return punchReport{}, err
	}
	punchStats, punchErr := tree.PunchFreeMmapPages()
	if punchErr != nil && !errors.Is(punchErr, pagebtree.ErrMmapHolePunchUnsupported) {
		return punchReport{}, punchErr
	}
	after, err := tree.MmapSpaceStats()
	if err != nil {
		return punchReport{}, err
	}
	report := punchReport{
		Path:             path,
		HolePunchProfile: pagebtree.MmapHolePunchProfile(),
		BeforeSpace:      before,
		AfterSpace:       after,
		PunchStats:       punchStats,
	}
	if punchErr != nil {
		report.Error = punchErr.Error()
	}
	return report, nil
}
