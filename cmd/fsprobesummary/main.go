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

type probeReport struct {
	Path         string          `json:"path"`
	KeysInserted int             `json:"keys_inserted"`
	KeysDeleted  int             `json:"keys_deleted"`
	ValueBytes   int             `json:"value_bytes"`
	Platform     probePlatform   `json:"platform"`
	AfterInsert  probePhase      `json:"after_insert"`
	AfterDelete  probePhase      `json:"after_delete"`
	AfterCompact probePhase      `json:"after_compact"`
	AfterPunch   probePhase      `json:"after_punch"`
	PunchStats   probePunchStats `json:"punch_stats"`
	PunchError   string          `json:"punch_error,omitempty"`
}

type probePlatform struct {
	GOOS string
}

type probePhase struct {
	Stats probeStats `json:"stats"`
	Space probeSpace `json:"space"`
}

type probeStats struct {
	Len          int
	FreePages    int
	RetiredPages int
}

type probeSpace struct {
	LogicalFileBytes int64
	AllocatedBytes   int64
	SparseBytes      int64
	FilesystemType   string
	FilesystemTypeID int64
	MountPath        string
	MountSource      string
	MountOptions     string
}

type probePunchStats struct {
	PunchedPages int
}

type reportInput struct {
	name   string
	reader io.Reader
}

type probeRow struct {
	report       string
	goos         string
	filesystem   string
	mount        string
	mountOptions string
	keys         string
	valueBytes   string
	phase        string
	length       string
	freePages    string
	retiredPages string
	logicalBytes string
	allocated    string
	sparse       string
	punchedPages string
	punchError   string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	inputs, closers, err := openInputs(args, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "fs probe summary: %v\n", err)
		return 1
	}
	for _, closer := range closers {
		defer closer.Close()
	}

	rows, err := parseProbeRows(inputs)
	if err != nil {
		fmt.Fprintf(stderr, "fs probe summary: %v\n", err)
		return 1
	}
	writeMarkdown(rows, stdout)
	return 0
}

func openInputs(args []string, stdin io.Reader) ([]reportInput, []io.Closer, error) {
	if len(args) == 0 {
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		return []reportInput{{name: "stdin", reader: stdin}}, nil, nil
	}
	inputs := make([]reportInput, 0, len(args))
	closers := make([]io.Closer, 0, len(args))
	for _, arg := range args {
		file, err := os.Open(arg)
		if err != nil {
			for _, closer := range closers {
				_ = closer.Close()
			}
			return nil, nil, err
		}
		inputs = append(inputs, reportInput{name: arg, reader: file})
		closers = append(closers, file)
	}
	return inputs, closers, nil
}

func parseProbeRows(inputs []reportInput) ([]probeRow, error) {
	var rows []probeRow
	for _, input := range inputs {
		report, err := decodeReport(input.reader)
		if err != nil {
			return nil, fmt.Errorf("%s: decode: %w", input.name, err)
		}
		rows = append(rows, rowsForReport(report, input.name)...)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no filesystem probe reports found")
	}
	return rows, nil
}

func decodeReport(reader io.Reader) (probeReport, error) {
	var report probeReport
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&report); err != nil {
		return probeReport{}, err
	}
	return report, nil
}

func rowsForReport(report probeReport, inputName string) []probeRow {
	reportName := filepath.Base(report.Path)
	if reportName == "." || reportName == string(filepath.Separator) || reportName == "" {
		reportName = filepath.Base(inputName)
	}
	common := probeRow{
		report:       reportName,
		goos:         report.Platform.GOOS,
		filesystem:   filesystemLabel(report.AfterInsert.Space),
		mount:        mountLabel(report.AfterInsert.Space),
		mountOptions: report.AfterInsert.Space.MountOptions,
		keys:         fmt.Sprintf("%d/%d", report.KeysInserted, report.KeysDeleted),
		valueBytes:   strconv.Itoa(report.ValueBytes),
	}
	return []probeRow{
		common.withPhase("after_insert", report.AfterInsert, ""),
		common.withPhase("after_delete", report.AfterDelete, ""),
		common.withPhase("after_compact", report.AfterCompact, ""),
		common.withPhase("after_punch", report.AfterPunch, intString(report.PunchStats.PunchedPages)).withPunchError(report.PunchError),
	}
}

func (row probeRow) withPhase(name string, phase probePhase, punchedPages string) probeRow {
	row.phase = name
	row.length = strconv.Itoa(phase.Stats.Len)
	row.freePages = strconv.Itoa(phase.Stats.FreePages)
	row.retiredPages = strconv.Itoa(phase.Stats.RetiredPages)
	row.logicalBytes = strconv.FormatInt(phase.Space.LogicalFileBytes, 10)
	row.allocated = strconv.FormatInt(phase.Space.AllocatedBytes, 10)
	row.sparse = strconv.FormatInt(phase.Space.SparseBytes, 10)
	row.punchedPages = punchedPages
	return row
}

func (row probeRow) withPunchError(err string) probeRow {
	row.punchError = err
	return row
}

func filesystemLabel(space probeSpace) string {
	switch {
	case space.FilesystemType != "" && space.FilesystemTypeID != 0:
		return fmt.Sprintf("%s (%d)", space.FilesystemType, space.FilesystemTypeID)
	case space.FilesystemType != "":
		return space.FilesystemType
	case space.FilesystemTypeID != 0:
		return strconv.FormatInt(space.FilesystemTypeID, 10)
	default:
		return ""
	}
}

func mountLabel(space probeSpace) string {
	switch {
	case space.MountPath != "" && space.MountSource != "":
		return fmt.Sprintf("%s [%s]", space.MountPath, space.MountSource)
	case space.MountPath != "":
		return space.MountPath
	default:
		return space.MountSource
	}
}

func intString(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func writeMarkdown(rows []probeRow, writer io.Writer) {
	fmt.Fprintln(writer, "| Report | GOOS | FS | Mount | Mount options | Keys | Value bytes | Phase | Len | Free pages | Retired pages | Logical bytes | Allocated bytes | Sparse bytes | Punched pages | Punch error |")
	fmt.Fprintln(writer, "| --- | --- | --- | --- | --- | ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |")
	for _, row := range rows {
		fmt.Fprintf(writer, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			markdownCell(row.report),
			markdownCell(row.goos),
			markdownCell(row.filesystem),
			markdownCell(row.mount),
			markdownCell(row.mountOptions),
			row.keys,
			row.valueBytes,
			markdownCell(row.phase),
			row.length,
			row.freePages,
			row.retiredPages,
			row.logicalBytes,
			row.allocated,
			row.sparse,
			row.punchedPages,
			markdownCell(row.punchError),
		)
	}
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
}
