package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

type readerProcess struct {
	cmd     *exec.Cmd
	ready   string
	release string
	tempDir string
	output  *bytes.Buffer
}

func startReaderProcesses(path string, count int) ([]readerProcess, error) {
	if count == 0 {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "mmaptxworkload-readers-*")
	if err != nil {
		return nil, err
	}
	processes := make([]readerProcess, 0, count)
	exe, err := os.Executable()
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	for i := 0; i < count; i++ {
		process := readerProcess{
			ready:   filepath.Join(dir, fmt.Sprintf("reader-%02d.ready", i)),
			release: filepath.Join(dir, fmt.Sprintf("reader-%02d.release", i)),
			tempDir: dir,
		}
		cmd := exec.Command(exe,
			"--child-reader",
			"--ready", process.ready,
			"--release", process.release,
			"--key", "seed",
			path,
		)
		output := &bytes.Buffer{}
		cmd.Stdout = output
		cmd.Stderr = output
		process.output = output
		if err := cmd.Start(); err != nil {
			cleanupReaderProcesses(processes)
			os.RemoveAll(dir)
			return nil, err
		}
		process.cmd = cmd
		processes = append(processes, process)
		if err := waitForFile(process.ready, 5*time.Second); err != nil {
			cleanupReaderProcesses(processes)
			os.RemoveAll(dir)
			return nil, fmt.Errorf("reader process %d did not become ready: %w; output: %s", i, err, output.String())
		}
	}
	return processes, nil
}

func releaseReaderProcesses(processes []readerProcess) error {
	defer removeReaderProcessDirs(processes)
	for _, process := range processes {
		if err := os.WriteFile(process.release, []byte("release"), 0o644); err != nil {
			return err
		}
		if err := process.cmd.Wait(); err != nil {
			return fmt.Errorf("reader process exit: %w; output: %s", err, process.output.String())
		}
	}
	return nil
}

func cleanupReaderProcesses(processes []readerProcess) {
	defer removeReaderProcessDirs(processes)
	for _, process := range processes {
		if process.cmd != nil && process.cmd.Process != nil {
			_ = process.cmd.Process.Kill()
			_ = process.cmd.Wait()
		}
	}
}

func removeReaderProcessDirs(processes []readerProcess) {
	removed := map[string]bool{}
	for _, process := range processes {
		if process.tempDir == "" || removed[process.tempDir] {
			continue
		}
		_ = os.RemoveAll(process.tempDir)
		removed[process.tempDir] = true
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

func runChildReader(options txWorkloadOptions) error {
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
