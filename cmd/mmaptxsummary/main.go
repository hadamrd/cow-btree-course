package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type txReport struct {
	Label                       string            `json:"label,omitempty"`
	Path                        string            `json:"path,omitempty"`
	Transactions                int               `json:"transactions"`
	DeleteEvery                 int               `json:"delete_every,omitempty"`
	Committed                   int               `json:"committed"`
	Conflicted                  int               `json:"conflicted"`
	DeletedCommittedKeys        int               `json:"deleted_committed_keys,omitempty"`
	ReopenedCommittedKeys       int               `json:"reopened_committed_keys"`
	ReopenedDeletedKeys         int               `json:"reopened_deleted_keys"`
	ReopenedConflictedKeys      int               `json:"reopened_conflicted_keys"`
	ReopenedOutsideKeys         int               `json:"reopened_outside_keys"`
	Readers                     int               `json:"readers,omitempty"`
	ActiveReadersObserved       int               `json:"active_readers_observed,omitempty"`
	ReaderPinnedRetiredPages    int               `json:"reader_pinned_retired_pages,omitempty"`
	ReaderPinnedFreePages       int               `json:"reader_pinned_free_pages,omitempty"`
	ReaderPinnedReclaimPressure txReclaimPressure `json:"reader_pinned_reclaim_pressure,omitempty"`
	ReaderProcesses             int               `json:"reader_processes,omitempty"`
	FinalStats                  txStats           `json:"final_stats"`
	TransactionConflictKind     string            `json:"transaction_conflict_kind"`
}

type txStats struct {
	Revision       uint64
	SyncedRevision uint64
	RetiredPages   int
	FreePages      int
}

type txReclaimPressure struct {
	ReaderPinnedRetiredPages int
	BlockedByReaders         bool
}

type txInput struct {
	name   string
	reader io.Reader
}

type txRow struct {
	report             string
	transactions       string
	deleteEvery        string
	readers            string
	committed          string
	conflicted         string
	deletedCommitted   string
	reopenedCommitted  string
	reopenedDeleted    string
	reopenedConflicted string
	outsideKeys        string
	syncedRevision     string
	pinnedRetired      string
	pinnedFree         string
	blockedByReaders   string
	finalRetired       string
	finalFree          string
	conflictKind       string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	inputs, closers, err := openInputs(args, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "mmap tx summary: %v\n", err)
		return 1
	}
	for _, closer := range closers {
		defer closer.Close()
	}

	rows, err := parseRows(inputs)
	if err != nil {
		fmt.Fprintf(stderr, "mmap tx summary: %v\n", err)
		return 1
	}
	writeMarkdown(rows, stdout)
	return 0
}

func openInputs(args []string, stdin io.Reader) ([]txInput, []io.Closer, error) {
	if len(args) == 0 {
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		return []txInput{{name: "stdin", reader: stdin}}, nil, nil
	}
	inputs := make([]txInput, 0, len(args))
	closers := make([]io.Closer, 0, len(args))
	for _, arg := range args {
		file, err := os.Open(arg)
		if err != nil {
			for _, closer := range closers {
				_ = closer.Close()
			}
			return nil, nil, err
		}
		inputs = append(inputs, txInput{name: arg, reader: file})
		closers = append(closers, file)
	}
	return inputs, closers, nil
}

func parseRows(inputs []txInput) ([]txRow, error) {
	rows := make([]txRow, 0, len(inputs))
	for _, input := range inputs {
		report, err := decodeReport(input.reader)
		if err != nil {
			return nil, fmt.Errorf("%s: decode: %w", input.name, err)
		}
		rows = append(rows, rowForReport(report, input.name))
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no mmap transaction workload reports found")
	}
	return rows, nil
}

func decodeReport(reader io.Reader) (txReport, error) {
	var report txReport
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&report); err != nil {
		return txReport{}, err
	}
	return report, nil
}

func rowForReport(report txReport, inputName string) txRow {
	return txRow{
		report:             reportName(report, inputName),
		transactions:       strconv.Itoa(report.Transactions),
		deleteEvery:        optionalInt(report.DeleteEvery),
		readers:            readersCell(report),
		committed:          strconv.Itoa(report.Committed),
		conflicted:         strconv.Itoa(report.Conflicted),
		deletedCommitted:   optionalInt(report.DeletedCommittedKeys),
		reopenedCommitted:  strconv.Itoa(report.ReopenedCommittedKeys),
		reopenedDeleted:    strconv.Itoa(report.ReopenedDeletedKeys),
		reopenedConflicted: strconv.Itoa(report.ReopenedConflictedKeys),
		outsideKeys:        strconv.Itoa(report.ReopenedOutsideKeys),
		syncedRevision:     fmt.Sprintf("%d/%d", report.FinalStats.Revision, report.FinalStats.SyncedRevision),
		pinnedRetired:      optionalInt(report.ReaderPinnedRetiredPages),
		pinnedFree:         optionalInt(report.ReaderPinnedFreePages),
		blockedByReaders:   optionalBool(report.ReaderPinnedReclaimPressure.BlockedByReaders),
		finalRetired:       strconv.Itoa(report.FinalStats.RetiredPages),
		finalFree:          strconv.Itoa(report.FinalStats.FreePages),
		conflictKind:       report.TransactionConflictKind,
	}
}

func readersCell(report txReport) string {
	if report.ReaderProcesses > 0 {
		return fmt.Sprintf("%d+%d/%d", report.Readers, report.ReaderProcesses, report.ActiveReadersObserved)
	}
	return fmt.Sprintf("%d/%d", report.Readers, report.ActiveReadersObserved)
}

func reportName(report txReport, inputName string) string {
	if report.Label != "" {
		return report.Label
	}
	if report.Path != "" {
		if base := filepath.Base(report.Path); base != "." && base != string(filepath.Separator) && base != "" {
			return base
		}
	}
	if inputName == "" {
		return "stdin"
	}
	return filepath.Base(inputName)
}

func optionalInt(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func optionalBool(value bool) string {
	if !value {
		return ""
	}
	return strconv.FormatBool(value)
}

func writeMarkdown(rows []txRow, writer io.Writer) {
	fmt.Fprintln(writer, "| Report | Tx | Delete every | Readers | Committed | Conflicted | Deleted committed | Reopened committed | Reopened deleted | Reopened conflicted | Outside keys | Synced revision | Pinned retired | Pinned free | Blocked by readers | Final retired | Final free | Conflict kind |")
	fmt.Fprintln(writer, "| --- | ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | --- | ---: | ---: | --- |")
	for _, row := range rows {
		fmt.Fprintf(writer, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			markdownCell(row.report),
			row.transactions,
			row.deleteEvery,
			markdownCell(row.readers),
			row.committed,
			row.conflicted,
			row.deletedCommitted,
			row.reopenedCommitted,
			row.reopenedDeleted,
			row.reopenedConflicted,
			row.outsideKeys,
			markdownCell(row.syncedRevision),
			row.pinnedRetired,
			row.pinnedFree,
			row.blockedByReaders,
			row.finalRetired,
			row.finalFree,
			markdownCell(row.conflictKind),
		)
	}
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
}
