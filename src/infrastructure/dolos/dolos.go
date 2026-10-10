// Package dolos compares source files with the Dolos command line tool.
package dolos

import (
	"context"
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

	ctx, cancel := context.WithTimeout(context.Background(), t.Timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "dolos", args...)
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("dolos: %w", err)
	}
	return ParseOutput(string(output), byName), nil
}

// ParseOutput reads the table Dolos prints: two file names and a similarity per row. Rows that
// are not a pair of the given files (headers, rulers) are skipped.
func ParseOutput(output string, byName map[string]string) []domain.Match {
	var matches []domain.Match
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		a, okA := byName[filepath.Base(fields[0])]
		b, okB := byName[filepath.Base(fields[1])]
		score, err := strconv.ParseFloat(fields[2], 64)
		if okA && okB && err == nil {
			matches = append(matches, domain.Match{A: a, B: b, Score: score})
		}
	}
	return matches
}
