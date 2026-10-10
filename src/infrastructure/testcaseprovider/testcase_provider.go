package testcaseprovider

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/domain"
)

type FileSystemTestCaseProvider struct {
	BasePath string
}

func NewFileSystemTestCaseProvider(basePath string) abstractions.TestCaseProvider {
	return &FileSystemTestCaseProvider{BasePath: basePath}
}

// GetTestCases pairs every <name>.in of the problem folder with its <name>.out, sorted by name.
func (p *FileSystemTestCaseProvider) GetTestCases(problemID int) ([]domain.TestCase, error) {
	testCases := []domain.TestCase{}
	dir := filepath.Join(p.BasePath, strconv.Itoa(problemID))

	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read test case directory: %w", err)
	}

	inputPaths := make(map[string]string)
	outputPaths := make(map[string]string)

	for _, file := range files {
		name := file.Name()
		fullPath := filepath.Join(dir, name)

		if strings.HasSuffix(name, ".in") {
			key := strings.TrimSuffix(name, ".in")
			inputPaths[key] = fullPath
		} else if strings.HasSuffix(name, ".out") {
			key := strings.TrimSuffix(name, ".out")
			outputPaths[key] = fullPath
		}
	}

	keys := []string{}
	for k := range inputPaths {
		if _, ok := outputPaths[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	for _, k := range keys {
		tc := domain.TestCase{
			Name:   k,
			Input:  inputPaths[k],
			Output: outputPaths[k],
		}
		testCases = append(testCases, tc)
	}

	return testCases, nil
}

func (p *FileSystemTestCaseProvider) Files(problemID int) domain.ProblemFiles {
	dir := filepath.Join(p.BasePath, strconv.Itoa(problemID))
	var files domain.ProblemFiles
	if info, err := os.Stat(filepath.Join(dir, "spj")); err == nil && info.Mode().IsRegular() {
		files.SpecialJudge = filepath.Join(dir, "spj")
	}
	if info, err := os.Stat(filepath.Join(dir, "grader.cpp")); err == nil && info.Mode().IsRegular() {
		files.Grader = filepath.Join(dir, "grader.cpp")
		files.Headers, _ = filepath.Glob(filepath.Join(dir, "*.h"))
	}
	return files
}
