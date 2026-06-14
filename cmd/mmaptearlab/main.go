package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

type tearReport struct {
	Path                string           `json:"path"`
	Mode                string           `json:"mode"`
	TornOffset          int64            `json:"torn_offset"`
	OlderRevision       uint64           `json:"older_revision"`
	NewerRevision       uint64           `json:"newer_revision"`
	RecoveredRevision   uint64           `json:"recovered_revision"`
	OlderRoot           pagebtree.PageID `json:"older_root"`
	NewerRoot           pagebtree.PageID `json:"newer_root"`
	RecoveredRoot       pagebtree.PageID `json:"recovered_root"`
	RecoveredOldKey     bool             `json:"recovered_old_key"`
	RecoveredNewKey     bool             `json:"recovered_new_key"`
	FellBackToOlderRoot bool             `json:"fell_back_to_older_root"`
	OpenError           string           `json:"open_error,omitempty"`
}

type tearOptions struct {
	path string
	mode string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	options, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "mmap tear lab: %v\n", err)
		printUsage(stderr)
		return 2
	}
	report, err := runTearLab(options)
	if err != nil {
		fmt.Fprintf(stderr, "mmap tear lab: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "mmap tear lab: encode report: %v\n", err)
		return 1
	}
	return 0
}

func parseArgs(args []string) (tearOptions, error) {
	options := tearOptions{mode: "metadata"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--mode":
			i++
			if i >= len(args) || args[i] == "" {
				return tearOptions{}, fmt.Errorf("--mode expects metadata or root")
			}
			options.mode = args[i]
		default:
			if strings.HasPrefix(arg, "--mode=") {
				options.mode = strings.TrimPrefix(arg, "--mode=")
				if options.mode == "" {
					return tearOptions{}, fmt.Errorf("--mode expects metadata or root")
				}
				continue
			}
			if len(arg) > 0 && arg[0] == '-' {
				return tearOptions{}, fmt.Errorf("unknown argument %q", arg)
			}
			if options.path != "" {
				return tearOptions{}, fmt.Errorf("expected one DB path")
			}
			options.path = arg
		}
	}
	if options.path == "" {
		return tearOptions{}, fmt.Errorf("expected one DB path")
	}
	if options.mode != "metadata" && options.mode != "root" {
		return tearOptions{}, fmt.Errorf("mode must be metadata or root")
	}
	return options, nil
}

func printUsage(stderr io.Writer) {
	fmt.Fprintf(stderr, "usage: mmaptearlab [--mode metadata|root] DB.db\n")
}

func runTearLab(options tearOptions) (tearReport, error) {
	older, newer, err := createTwoRevisionMmap(options.path)
	if err != nil {
		return tearReport{}, err
	}
	report := tearReport{
		Path:          options.path,
		Mode:          options.mode,
		OlderRevision: older.Revision,
		NewerRevision: newer.Revision,
		OlderRoot:     older.Root,
		NewerRoot:     newer.Root,
	}
	switch options.mode {
	case "metadata":
		report.TornOffset = int64(newer.Revision%2) * pagebtree.PageSize
	case "root":
		report.TornOffset = int64(newer.Root)*pagebtree.PageSize + pagebtree.PageSize - 1
	}
	if err := tearByte(options.path, report.TornOffset); err != nil {
		return tearReport{}, err
	}

	reopened, err := pagebtree.OpenMmap(options.path, pagebtree.MmapOptions{})
	if err != nil {
		report.OpenError = err.Error()
		return report, nil
	}
	defer reopened.Close()

	stats := reopened.Stats()
	report.RecoveredRevision = stats.Revision
	report.RecoveredRoot = stats.Root
	oldValue, oldOK := reopened.Get("alpha")
	newValue, newOK := reopened.Get("bravo")
	report.RecoveredOldKey = oldOK && string(oldValue) == "one"
	report.RecoveredNewKey = newOK && string(newValue) == "two"
	report.FellBackToOlderRoot = report.RecoveredRevision == report.OlderRevision &&
		report.RecoveredRoot == report.OlderRoot &&
		report.RecoveredOldKey &&
		!report.RecoveredNewKey
	return report, nil
}

func createTwoRevisionMmap(path string) (pagebtree.Stats, pagebtree.Stats, error) {
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		return pagebtree.Stats{}, pagebtree.Stats{}, err
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		tree.Close()
		return pagebtree.Stats{}, pagebtree.Stats{}, err
	}
	older := tree.Stats()
	tree.Put("bravo", []byte("two"))
	if err := tree.Sync(); err != nil {
		tree.Close()
		return pagebtree.Stats{}, pagebtree.Stats{}, err
	}
	newer := tree.Stats()
	if err := tree.Close(); err != nil {
		return pagebtree.Stats{}, pagebtree.Stats{}, err
	}
	if older.Root == 0 || newer.Root == 0 || older.Root == newer.Root {
		return pagebtree.Stats{}, pagebtree.Stats{}, fmt.Errorf("unexpected roots older=%d newer=%d", older.Root, newer.Root)
	}
	return older, newer, nil
}

func tearByte(path string, offset int64) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteAt([]byte{0xff}, offset); err != nil {
		return err
	}
	return file.Sync()
}
