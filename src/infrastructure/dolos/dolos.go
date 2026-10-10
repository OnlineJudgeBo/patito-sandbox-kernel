// Package dolos compares source files with the Dolos command line tool.
package dolos

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/samuelloza/isolate-wrapper/src/domain"
)

// Tool runs "dolos run" on a copy of the files. Dolos only reads them, it never executes them.
type Tool struct {
	Timeout time.Duration
}

func (t *Tool) Compare(files []string) ([]domain.Match, error) {
	dir, err := os.MkdirTemp("", "patito-similarity-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	byName := map[string]string{}
	args := []string{"run"}
	for _, file := range files {
		name := filepath.Base(file)
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			return nil, err
		}
		byName[name] = file
		args = append(args, name)
	}

	// The CSV report has the same shape for any number of files; the table Dolos prints does not.
	args = append(args[:1], append([]string{"--output-format", "csv", "--output-destination", "report"}, args[1:]...)...)
	ctx, cancel := context.WithTimeout(context.Background(), t.Timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "dolos", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("dolos: %w: %s", err, strings.TrimSpace(string(output)))
	}
	pairs, err := os.ReadFile(filepath.Join(dir, "report", "pairs.csv"))
	if err != nil {
		return nil, err
	}
	return ParsePairs(string(pairs), byName)
}

// ParsePairs reads pairs.csv of a Dolos report. byName maps the file names it was given to their paths.
func ParsePairs(pairs string, byName map[string]string) ([]domain.Match, error) {
	rows, err := csv.NewReader(strings.NewReader(pairs)).ReadAll()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	column := map[string]int{}
	for index, name := range rows[0] {
		column[name] = index
	}
	left, okLeft := column["leftFilePath"]
	right, okRight := column["rightFilePath"]
	similarity, okSimilarity := column["similarity"]
	if !okLeft || !okRight || !okSimilarity {
		return nil, fmt.Errorf("dolos: unexpected columns in pairs.csv: %v", rows[0])
	}
	var matches []domain.Match
	for _, row := range rows[1:] {
		a, okA := byName[filepath.Base(row[left])]
		b, okB := byName[filepath.Base(row[right])]
		score, err := strconv.ParseFloat(row[similarity], 64)
		if okA && okB && err == nil {
			matches = append(matches, domain.Match{A: a, B: b, Score: score})
		}
	}
	return matches, nil
}
