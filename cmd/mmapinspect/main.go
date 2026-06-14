package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

type inspectReport struct {
	Valid            bool               `json:"valid"`
	Error            string             `json:"error,omitempty"`
	Stats            pagebtree.Stats    `json:"stats"`
	ReachablePageIDs []pagebtree.PageID `json:"reachable_page_ids"`
	FreePageIDs      []pagebtree.PageID `json:"free_page_ids"`
	RetiredPageIDs   []pagebtree.PageID `json:"retired_page_ids"`
	LeafLinksChecked bool               `json:"leaf_links_checked"`
	LeafLinksSkipped bool               `json:"leaf_links_skipped"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintf(stderr, "usage: mmapinspect DB.db\n")
		return 2
	}

	tree, err := pagebtree.OpenMmapReadOnly(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "mmap inspect: %v\n", err)
		return 1
	}
	defer tree.Close()

	report := inspectFromAudit(tree.Audit())
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

func inspectFromAudit(audit pagebtree.AuditReport) inspectReport {
	report := inspectReport{
		Valid:            audit.Valid(),
		Stats:            audit.Stats,
		ReachablePageIDs: audit.ReachablePageIDs,
		FreePageIDs:      audit.FreePageIDs,
		RetiredPageIDs:   audit.RetiredPageIDs,
		LeafLinksChecked: audit.LeafLinksChecked,
		LeafLinksSkipped: audit.LeafLinksSkipped,
	}
	if audit.Error != nil {
		report.Error = audit.Error.Error()
	}
	return report
}
