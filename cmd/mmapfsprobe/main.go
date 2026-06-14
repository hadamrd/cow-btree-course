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

type fsProbeReport struct {
	Path         string                           `json:"path,omitempty"`
	PathRedacted bool                             `json:"path_redacted,omitempty"`
	Label        string                           `json:"label,omitempty"`
	KeysInserted int                              `json:"keys_inserted"`
	KeysDeleted  int                              `json:"keys_deleted"`
	ValueBytes   int                              `json:"value_bytes"`
	Platform     pagebtree.MmapPlatformCapability `json:"platform"`
	AfterInsert  fsProbePhase                     `json:"after_insert"`
	AfterDelete  fsProbePhase                     `json:"after_delete"`
	AfterCompact fsProbePhase                     `json:"after_compact"`
	AfterPunch   fsProbePhase                     `json:"after_punch"`
	PunchStats   pagebtree.MmapHolePunchStats     `json:"punch_stats"`
	PunchError   string                           `json:"punch_error,omitempty"`
}

type fsProbePhase struct {
	Stats pagebtree.Stats          `json:"stats"`
	Space pagebtree.MmapSpaceStats `json:"space"`
}

type fsProbeOptions struct {
	path       string
	label      string
	redactPath bool
	keys       int
	valueBytes int
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	options, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "mmap fs probe: %v\n", err)
		printUsage(stderr)
		return 2
	}
	report, err := runProbe(options)
	if err != nil {
		fmt.Fprintf(stderr, "mmap fs probe: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "mmap fs probe: encode report: %v\n", err)
		return 1
	}
	return 0
}

func parseArgs(args []string) (fsProbeOptions, error) {
	options := fsProbeOptions{
		keys:       128,
		valueBytes: 512,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--keys":
			i++
			if i >= len(args) {
				return fsProbeOptions{}, fmt.Errorf("--keys expects a value")
			}
			value, err := strconv.Atoi(args[i])
			if err != nil {
				return fsProbeOptions{}, fmt.Errorf("--keys expects an integer")
			}
			options.keys = value
		case strings.HasPrefix(arg, "--keys="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--keys="))
			if err != nil {
				return fsProbeOptions{}, fmt.Errorf("--keys expects an integer")
			}
			options.keys = value
		case arg == "--value-bytes":
			i++
			if i >= len(args) {
				return fsProbeOptions{}, fmt.Errorf("--value-bytes expects a value")
			}
			value, err := strconv.Atoi(args[i])
			if err != nil {
				return fsProbeOptions{}, fmt.Errorf("--value-bytes expects an integer")
			}
			options.valueBytes = value
		case strings.HasPrefix(arg, "--value-bytes="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--value-bytes="))
			if err != nil {
				return fsProbeOptions{}, fmt.Errorf("--value-bytes expects an integer")
			}
			options.valueBytes = value
		case arg == "--label":
			i++
			if i >= len(args) {
				return fsProbeOptions{}, fmt.Errorf("--label expects a value")
			}
			options.label = args[i]
		case strings.HasPrefix(arg, "--label="):
			options.label = strings.TrimPrefix(arg, "--label=")
		case arg == "--redact-path":
			options.redactPath = true
		case strings.HasPrefix(arg, "-"):
			return fsProbeOptions{}, fmt.Errorf("unknown argument %q", arg)
		default:
			if options.path != "" {
				return fsProbeOptions{}, fmt.Errorf("expected one DB path")
			}
			options.path = arg
		}
	}
	if options.path == "" {
		return fsProbeOptions{}, fmt.Errorf("expected one DB path")
	}
	if options.keys < 4 {
		return fsProbeOptions{}, fmt.Errorf("--keys must be at least 4")
	}
	if options.valueBytes < 1 {
		return fsProbeOptions{}, fmt.Errorf("--value-bytes must be positive")
	}
	if strings.TrimSpace(options.label) != options.label || (options.label == "" && labelWasExplicit(args)) {
		return fsProbeOptions{}, fmt.Errorf("--label must be non-empty and must not have leading or trailing whitespace")
	}
	return options, nil
}

func printUsage(stderr io.Writer) {
	fmt.Fprintf(stderr, "usage: mmapfsprobe [--keys N] [--value-bytes N] [--label NAME] [--redact-path] DB.db\n")
}

func labelWasExplicit(args []string) bool {
	for _, arg := range args {
		if arg == "--label" || strings.HasPrefix(arg, "--label=") {
			return true
		}
	}
	return false
}

func runProbe(options fsProbeOptions) (fsProbeReport, error) {
	if err := refuseExistingArtifacts(options.path); err != nil {
		return fsProbeReport{}, err
	}

	tree, err := pagebtree.OpenMmap(options.path, pagebtree.MmapOptions{
		Degree:   2,
		MaxPages: options.keys * 8,
	})
	if err != nil {
		return fsProbeReport{}, err
	}
	defer tree.Close()

	value := bytesOf('v', options.valueBytes)
	for i := 0; i < options.keys; i++ {
		tree.Put(probeKey(i), value)
	}
	if err := tree.Sync(); err != nil {
		return fsProbeReport{}, err
	}
	afterInsert, err := collectPhase(tree)
	if err != nil {
		return fsProbeReport{}, err
	}

	keysDeleted := 0
	for i := 1; i < options.keys; i += 2 {
		if _, deleted := tree.Delete(probeKey(i)); deleted {
			keysDeleted++
		}
	}
	if err := tree.Sync(); err != nil {
		return fsProbeReport{}, err
	}
	afterDelete, err := collectPhase(tree)
	if err != nil {
		return fsProbeReport{}, err
	}

	if err := tree.Compact(); err != nil {
		return fsProbeReport{}, err
	}
	if err := tree.Sync(); err != nil {
		return fsProbeReport{}, err
	}
	afterCompact, err := collectPhase(tree)
	if err != nil {
		return fsProbeReport{}, err
	}

	punchStats, punchErr := tree.PunchFreeMmapPages()
	if punchErr != nil && punchErr != pagebtree.ErrMmapHolePunchUnsupported {
		return fsProbeReport{}, punchErr
	}
	afterPunch, err := collectPhase(tree)
	if err != nil {
		return fsProbeReport{}, err
	}

	report := fsProbeReport{
		Path:         options.path,
		PathRedacted: options.redactPath,
		Label:        options.label,
		KeysInserted: options.keys,
		KeysDeleted:  keysDeleted,
		ValueBytes:   options.valueBytes,
		Platform:     pagebtree.MmapPlatformProfile(),
		AfterInsert:  afterInsert,
		AfterDelete:  afterDelete,
		AfterCompact: afterCompact,
		AfterPunch:   afterPunch,
		PunchStats:   punchStats,
	}
	if punchErr != nil {
		report.PunchError = punchErr.Error()
	}
	if options.redactPath {
		report.Path = ""
	}
	return report, nil
}

func collectPhase(tree *pagebtree.Tree) (fsProbePhase, error) {
	space, err := tree.MmapSpaceStats()
	if err != nil {
		return fsProbePhase{}, err
	}
	return fsProbePhase{
		Stats: tree.Stats(),
		Space: space,
	}, nil
}

func refuseExistingArtifacts(path string) error {
	for _, name := range []string{path, path + ".writer", path + ".readers"} {
		if _, err := os.Stat(name); err == nil {
			return fmt.Errorf("%s already exists; choose a disposable empty path", name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func bytesOf(value byte, count int) []byte {
	data := make([]byte, count)
	for i := range data {
		data[i] = value
	}
	return data
}

func probeKey(i int) string {
	return fmt.Sprintf("key-%06d", i)
}
