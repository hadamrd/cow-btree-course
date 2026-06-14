package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

const (
	defaultReaders = 4
	defaultRounds  = 3
	defaultKeys    = 64
)

type soakReport struct {
	Path                         string                    `json:"path"`
	Readers                      int                       `json:"readers"`
	Rounds                       int                       `json:"rounds"`
	Keys                         int                       `json:"keys"`
	ActiveReadersObserved        int                       `json:"active_readers_observed"`
	ActiveReadersAfterOneRelease int                       `json:"active_readers_after_one_release"`
	PinnedRounds                 int                       `json:"pinned_rounds"`
	MaxRetiredPagesWhilePinned   int                       `json:"max_retired_pages_while_pinned"`
	MaxFreePagesWhilePinned      int                       `json:"max_free_pages_while_pinned"`
	FinalRetiredPages            int                       `json:"final_retired_pages"`
	FinalFreePages               int                       `json:"final_free_pages"`
	ReaderStatsWhilePinned       pagebtree.MmapReaderStats `json:"reader_stats_while_pinned"`
	ReaderStatsAfterRelease      pagebtree.MmapReaderStats `json:"reader_stats_after_release"`
}

type soakOptions struct {
	path         string
	readers      int
	rounds       int
	keys         int
	child        bool
	childReady   string
	childRelease string
	childKey     string
}

type readerChild struct {
	cmd     *exec.Cmd
	ready   string
	release string
	output  *bytes.Buffer
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	options, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "mmap reader soak: %v\n", err)
		printUsage(stderr)
		return 2
	}
	if options.child {
		if err := runChildReader(options); err != nil {
			fmt.Fprintf(stderr, "mmap reader soak child: %v\n", err)
			return 1
		}
		return 0
	}

	report, err := runSoak(options)
	if err != nil {
		fmt.Fprintf(stderr, "mmap reader soak: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "mmap reader soak: encode report: %v\n", err)
		return 1
	}
	return 0
}

func parseArgs(args []string) (soakOptions, error) {
	options := soakOptions{
		readers: defaultReaders,
		rounds:  defaultRounds,
		keys:    defaultKeys,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--readers":
			value, ok := nextArg(args, &i)
			if !ok {
				return soakOptions{}, fmt.Errorf("--readers expects a positive integer")
			}
			readers, err := parsePositiveInt(value)
			if err != nil {
				return soakOptions{}, fmt.Errorf("--readers expects a positive integer: %w", err)
			}
			options.readers = readers
		case "--rounds":
			value, ok := nextArg(args, &i)
			if !ok {
				return soakOptions{}, fmt.Errorf("--rounds expects a positive integer")
			}
			rounds, err := parsePositiveInt(value)
			if err != nil {
				return soakOptions{}, fmt.Errorf("--rounds expects a positive integer: %w", err)
			}
			options.rounds = rounds
		case "--keys":
			value, ok := nextArg(args, &i)
			if !ok {
				return soakOptions{}, fmt.Errorf("--keys expects a positive integer")
			}
			keys, err := parsePositiveInt(value)
			if err != nil {
				return soakOptions{}, fmt.Errorf("--keys expects a positive integer: %w", err)
			}
			options.keys = keys
		case "--child-reader":
			options.child = true
		case "--ready":
			value, ok := nextArg(args, &i)
			if !ok || value == "" {
				return soakOptions{}, fmt.Errorf("--ready expects a path")
			}
			options.childReady = value
		case "--release":
			value, ok := nextArg(args, &i)
			if !ok || value == "" {
				return soakOptions{}, fmt.Errorf("--release expects a path")
			}
			options.childRelease = value
		case "--key":
			value, ok := nextArg(args, &i)
			if !ok || value == "" {
				return soakOptions{}, fmt.Errorf("--key expects a key")
			}
			options.childKey = value
		default:
			if strings.HasPrefix(arg, "--readers=") {
				readers, err := parsePositiveInt(strings.TrimPrefix(arg, "--readers="))
				if err != nil {
					return soakOptions{}, fmt.Errorf("--readers expects a positive integer: %w", err)
				}
				options.readers = readers
				continue
			}
			if strings.HasPrefix(arg, "--rounds=") {
				rounds, err := parsePositiveInt(strings.TrimPrefix(arg, "--rounds="))
				if err != nil {
					return soakOptions{}, fmt.Errorf("--rounds expects a positive integer: %w", err)
				}
				options.rounds = rounds
				continue
			}
			if strings.HasPrefix(arg, "--keys=") {
				keys, err := parsePositiveInt(strings.TrimPrefix(arg, "--keys="))
				if err != nil {
					return soakOptions{}, fmt.Errorf("--keys expects a positive integer: %w", err)
				}
				options.keys = keys
				continue
			}
			if len(arg) > 0 && arg[0] == '-' {
				return soakOptions{}, fmt.Errorf("unknown argument %q", arg)
			}
			if options.path != "" {
				return soakOptions{}, fmt.Errorf("expected one DB path")
			}
			options.path = arg
		}
	}
	if options.path == "" {
		return soakOptions{}, fmt.Errorf("expected one DB path")
	}
	if options.child {
		if options.childReady == "" || options.childRelease == "" || options.childKey == "" {
			return soakOptions{}, fmt.Errorf("child reader requires --ready, --release, and --key")
		}
	}
	return options, nil
}

func nextArg(args []string, index *int) (string, bool) {
	*index = *index + 1
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
	fmt.Fprintf(stderr, "usage: mmapreadersoak [--readers N] [--rounds N] [--keys N] DB.db\n")
}

func runSoak(options soakOptions) (soakReport, error) {
	if err := seedSoakDatabase(options); err != nil {
		return soakReport{}, err
	}

	dir, err := os.MkdirTemp("", "mmapreadersoak-*")
	if err != nil {
		return soakReport{}, err
	}
	defer os.RemoveAll(dir)

	children, err := startReaderChildren(options, dir)
	if err != nil {
		cleanupChildren(children)
		return soakReport{}, err
	}
	defer cleanupChildren(children)

	writer, err := pagebtree.OpenMmap(options.path, pagebtree.MmapOptions{Degree: 2, MaxPages: maxPagesForSoak(options.keys)})
	if err != nil {
		return soakReport{}, err
	}
	defer writer.Close()

	readerStats, err := writer.MmapReaderStats()
	if err != nil {
		return soakReport{}, err
	}
	report := soakReport{
		Path:                   options.path,
		Readers:                options.readers,
		Rounds:                 options.rounds,
		Keys:                   options.keys,
		ActiveReadersObserved:  readerStats.ActiveSlots,
		ReaderStatsWhilePinned: readerStats,
	}

	for round := 0; round < options.rounds; round++ {
		for key := 0; key < options.keys; key++ {
			writer.Put(soakKey(key), []byte(fmt.Sprintf("round-%03d-value-%04d", round, key)))
		}
		stats := writer.Stats()
		if stats.RetiredPages > 0 && stats.FreePages == 0 {
			report.PinnedRounds++
		}
		if stats.RetiredPages > report.MaxRetiredPagesWhilePinned {
			report.MaxRetiredPagesWhilePinned = stats.RetiredPages
		}
		if stats.FreePages > report.MaxFreePagesWhilePinned {
			report.MaxFreePagesWhilePinned = stats.FreePages
		}
	}

	if len(children) > 0 {
		if err := releaseReaderChild(children[0]); err != nil {
			return soakReport{}, err
		}
		children = children[1:]
		stats, err := writer.MmapReaderStats()
		if err != nil {
			return soakReport{}, err
		}
		report.ActiveReadersAfterOneRelease = stats.ActiveSlots
	} else {
		report.ActiveReadersAfterOneRelease = readerStats.ActiveSlots
	}

	for _, child := range children {
		if err := releaseReaderChild(child); err != nil {
			return soakReport{}, err
		}
	}
	children = nil

	writer.Put("soak-final-reclaim-trigger", []byte("done"))
	final := writer.Stats()
	report.FinalRetiredPages = final.RetiredPages
	report.FinalFreePages = final.FreePages
	afterRelease, err := writer.MmapReaderStats()
	if err != nil {
		return soakReport{}, err
	}
	report.ReaderStatsAfterRelease = afterRelease
	if err := writer.Sync(); err != nil {
		return soakReport{}, err
	}
	return report, nil
}

func seedSoakDatabase(options soakOptions) error {
	tree, err := pagebtree.OpenMmap(options.path, pagebtree.MmapOptions{Degree: 2, MaxPages: maxPagesForSoak(options.keys)})
	if err != nil {
		return err
	}
	for i := 0; i < options.keys; i++ {
		tree.Put(soakKey(i), []byte(fmt.Sprintf("seed-value-%04d", i)))
	}
	if err := tree.Sync(); err != nil {
		tree.Close()
		return err
	}
	return tree.Close()
}

func startReaderChildren(options soakOptions, dir string) ([]readerChild, error) {
	children := make([]readerChild, 0, options.readers)
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	for i := 0; i < options.readers; i++ {
		child := readerChild{
			ready:   filepath.Join(dir, fmt.Sprintf("reader-%02d.ready", i)),
			release: filepath.Join(dir, fmt.Sprintf("reader-%02d.release", i)),
		}
		cmd := exec.Command(exe,
			"--child-reader",
			"--ready", child.ready,
			"--release", child.release,
			"--key", soakKey(i%options.keys),
			options.path,
		)
		output := &bytes.Buffer{}
		cmd.Stdout = output
		cmd.Stderr = output
		child.output = output
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		child.cmd = cmd
		children = append(children, child)
		if err := waitForFile(child.ready, 5*time.Second); err != nil {
			return children, fmt.Errorf("child reader %d did not become ready: %w; output: %s", i, err, output.String())
		}
	}
	return children, nil
}

func releaseReaderChild(child readerChild) error {
	if err := os.WriteFile(child.release, []byte("release"), 0o644); err != nil {
		return err
	}
	if err := child.cmd.Wait(); err != nil {
		return fmt.Errorf("child reader exit: %w; output: %s", err, child.output.String())
	}
	return nil
}

func cleanupChildren(children []readerChild) {
	for _, child := range children {
		if child.cmd != nil && child.cmd.Process != nil {
			_ = child.cmd.Process.Kill()
			_ = child.cmd.Wait()
		}
	}
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runChildReader(options soakOptions) error {
	reader, err := pagebtree.OpenMmapReadOnly(options.path)
	if err != nil {
		return err
	}
	defer reader.Close()
	value, ok := reader.Get(options.childKey)
	if !ok || len(value) == 0 {
		return fmt.Errorf("child key %q missing", options.childKey)
	}
	if err := os.WriteFile(options.childReady, []byte("ready"), 0o644); err != nil {
		return err
	}
	return waitForFile(options.childRelease, 24*time.Hour)
}

func soakKey(index int) string {
	return fmt.Sprintf("soak-key-%04d", index)
}

func maxPagesForSoak(keys int) int {
	pages := keys * 4
	if pages < 128 {
		return 128
	}
	return pages
}
