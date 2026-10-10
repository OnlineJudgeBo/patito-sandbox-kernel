// Package sourcearchive keeps accepted contest sources on disk, in the layout of the previous judge,
// and reads the similarity requests the admin API leaves next to them.
package sourcearchive

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/domain"
)

const requestFile = "similarity.request"

// Archive is rooted at <judge home>/data/contests.
type Archive struct {
	Root string
}

func (a *Archive) Keep(submission domain.Submission, extension string) error {
	dir := filepath.Join(a.Root, strconv.Itoa(submission.ContestID), "problem", strconv.Itoa(submission.ProblemID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.%s", submission.ID, extension)), []byte(submission.Source), 0644)
}

func (a *Archive) Requested() ([]int, error) {
	paths, err := filepath.Glob(filepath.Join(a.Root, "*", requestFile))
	var contests []int
	for _, path := range paths {
		if id, err := strconv.Atoi(filepath.Base(filepath.Dir(path))); err == nil {
			contests = append(contests, id)
		}
	}
	return contests, err
}

func (a *Archive) Finish(contestID int) error {
	return os.Remove(filepath.Join(a.Root, strconv.Itoa(contestID), requestFile))
}

func (a *Archive) Sources(contestID int) ([]abstractions.SourceGroup, error) {
	paths, err := filepath.Glob(filepath.Join(a.Root, strconv.Itoa(contestID), "problem", "*", "*.*"))
	if err != nil {
		return nil, err
	}
	groups := map[string]*abstractions.SourceGroup{}
	var order []string
	for _, path := range paths {
		name := filepath.Base(path)
		solution, extension, _ := strings.Cut(name, ".")
		solutionID, errSolution := strconv.Atoi(solution)
		problemID, errProblem := strconv.Atoi(filepath.Base(filepath.Dir(path)))
		if errSolution != nil || errProblem != nil {
			continue
		}
		key := fmt.Sprintf("%d.%s", problemID, extension)
		if groups[key] == nil {
			groups[key] = &abstractions.SourceGroup{ProblemID: problemID, Files: map[int]string{}}
			order = append(order, key)
		}
		groups[key].Files[solutionID] = path
	}
	result := make([]abstractions.SourceGroup, 0, len(order))
	for _, key := range order {
		result = append(result, *groups[key])
	}
	return result, nil
}
