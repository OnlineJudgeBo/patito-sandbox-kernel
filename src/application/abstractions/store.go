package abstractions

import "github.com/samuelloza/isolate-wrapper/src/domain"

// Store is the judge's view of the database the web, the API and the IDE write submissions to.
type Store interface {
	// Pending returns ids waiting to be judged, oldest first, restricted to the given languages.
	Pending(limit int, languages []int) ([]int, error)
	// Claim marks a submission as being compiled. False means another judge took it first.
	Claim(id int) (bool, error)
	Load(id int) (domain.Submission, error)
	SetRunning(id int) error
	Save(submission domain.Submission, result domain.Result) error
}

// TestCaseProvider lists the tests of a problem.
type TestCaseProvider interface {
	GetTestCases(problemID int) ([]domain.TestCase, error)
}

// Comparator compares the expected output with the one the program produced.
// It returns domain.OJ_AC, domain.OJ_PE or domain.OJ_WA.
type Comparator interface {
	Compare(expectedPath string, outputPath string) (int, error)
}
