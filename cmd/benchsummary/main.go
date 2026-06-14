package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

type benchmarkRow struct {
	name        string
	iterations  string
	nsPerOp     string
	bytesPerOp  string
	allocsPerOp string
	keysPerOp   string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		fmt.Fprintln(stderr, "usage: benchsummary [bench.out]")
		return 2
	}

	reader := stdin
	var file *os.File
	if len(args) == 1 {
		var err error
		file, err = os.Open(args[0])
		if err != nil {
			fmt.Fprintf(stderr, "benchsummary: %v\n", err)
			return 1
		}
		defer file.Close()
		reader = file
	}
	rows, err := parseBenchmarkRows(reader)
	if err != nil {
		fmt.Fprintf(stderr, "benchsummary: %v\n", err)
		return 1
	}
	writeMarkdown(rows, stdout)
	return 0
}

func parseBenchmarkRows(reader io.Reader) ([]benchmarkRow, error) {
	if reader == nil {
		reader = strings.NewReader("")
	}
	var rows []benchmarkRow
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "Benchmark") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		row := benchmarkRow{
			name:       trimBenchmarkName(fields[0]),
			iterations: fields[1],
		}
		for i := 2; i+1 < len(fields); i += 2 {
			value, unit := fields[i], fields[i+1]
			switch unit {
			case "ns/op":
				row.nsPerOp = value
			case "B/op":
				row.bytesPerOp = value
			case "allocs/op":
				row.allocsPerOp = value
			case "keys/op":
				row.keysPerOp = value
			}
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no benchmark rows found")
	}
	return rows, nil
}

func trimBenchmarkName(raw string) string {
	name := strings.TrimPrefix(raw, "Benchmark")
	if dash := strings.LastIndex(name, "-"); dash >= 0 {
		name = name[:dash]
	}
	return name
}

func writeMarkdown(rows []benchmarkRow, writer io.Writer) {
	fmt.Fprintln(writer, "| Benchmark | Iterations | ns/op | B/op | allocs/op | keys/op |")
	fmt.Fprintln(writer, "| --- | ---: | ---: | ---: | ---: | ---: |")
	for _, row := range rows {
		fmt.Fprintf(writer, "| %s | %s | %s | %s | %s | %s |\n",
			row.name,
			row.iterations,
			row.nsPerOp,
			row.bytesPerOp,
			row.allocsPerOp,
			row.keysPerOp,
		)
	}
}
