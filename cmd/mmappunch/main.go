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
	HolePunchProfile pagebtree.MmapHolePunchCapability `json:"hole_punch_profile"`
	BeforeSpace      pagebtree.MmapSpaceStats          `json:"before_space"`
	AfterSpace       pagebtree.MmapSpaceStats          `json:"after_space"`
	PunchStats       pagebtree.MmapHolePunchStats      `json:"punch_stats"`
	Error            string                            `json:"error,omitempty"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintf(stderr, "usage: mmappunch DB.db\n")
		return 2
	}
	report, err := punchMmapFile(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "mmap punch: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "mmap punch: encode report: %v\n", err)
		return 1
	}
	return 0
}

func punchMmapFile(path string) (punchReport, error) {
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{})
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
