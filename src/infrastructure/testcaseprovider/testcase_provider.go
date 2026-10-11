package testcaseprovider

import (
	"encoding/json"
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
	for _, name := range []string{"checker.cpp", "checker_cms.cpp"} {
		if !isFile(filepath.Join(dir, name)) {
			continue
		}
		files.Checker = domain.Checker{Source: filepath.Join(dir, name), CMS: name == "checker_cms.cpp",
			Binary: filepath.Join(dir, "checker"), Log: filepath.Join(dir, "checker.log")}
		if isFile(filepath.Join(dir, "testlib.h")) {
			files.Checker.Header = filepath.Join(dir, "testlib.h")
		}
		break
	}
	graders, _ := filepath.Glob(filepath.Join(dir, "grader.*"))
	for _, grader := range graders {
		if isFile(grader) {
			files.Graders = append(files.Graders, grader)
		}
	}
	if len(files.Graders) > 0 {
		headers, _ := filepath.Glob(filepath.Join(dir, "*.h"))
		for _, header := range headers {
			if filepath.Base(header) != "testlib.h" {
				files.Headers = append(files.Headers, header)
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "scoring.json")); err == nil {
		files.Groups = ParseGroups(data)
	}
	return files
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// ParseGroups reads scoring.json:
//
//	{ "groups": [ { "name": "Subtarea 1", "points": 10, "type": "sum", "tests": "s1_*" },
//	              { "points": 20, "type": "min", "tests": ["s2_*", "extra"] } ] }
//
// A malformed group is skipped and a file that is not JSON gives no groups, like the previous judge.
func ParseGroups(data []byte) []domain.ScoreGroup {
	var file struct {
		Groups []json.RawMessage `json:"groups"`
	}
	if json.Unmarshal(data, &file) != nil {
		return nil
	}
	var groups []domain.ScoreGroup
	for _, raw := range file.Groups {
		var item struct {
			Points *float64        `json:"points"`
			Type   string          `json:"type"`
			Tests  json.RawMessage `json:"tests"`
		}
		if json.Unmarshal(raw, &item) != nil || item.Points == nil || *item.Points < 0 {
			continue
		}
		var patterns []string
		var single string
		if json.Unmarshal(item.Tests, &single) == nil {
			patterns = []string{single}
		} else if json.Unmarshal(item.Tests, &patterns) != nil {
			continue
		}
		if len(patterns) == 0 {
			continue
		}
		if item.Type != "min" && item.Type != "mul" {
			item.Type = "sum"
		}
		groups = append(groups, domain.ScoreGroup{Points: *item.Points, Type: item.Type, Patterns: patterns})
	}
	return groups
}
