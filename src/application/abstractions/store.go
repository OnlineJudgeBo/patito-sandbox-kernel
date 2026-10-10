package abstractions

import (
	"time"

	"github.com/samuelloza/isolate-wrapper/src/domain"
)

// Store is the judge's view of the database the web, the API and the IDE write submissions to.
type Store interface {
	// Pending returns ids waiting to be judged, oldest first, restricted to the given languages.
	Pending(limit int, languages []int) ([]int, error)
	// Claim marks a submission as being compiled. False means another judge took it first.
	Claim(id int) (bool, error)
	Load(id int) (domain.Submission, error)
	SetRunning(id int) error
	Save(submission domain.Submission, result domain.Result) error
	// RecoverStuck returns to the queue the submissions a judge claimed longer ago than the
	// given time and never finished (it crashed or was killed), and reports how many.
	RecoverStuck(olderThan time.Duration) (int, error)
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

// SourceArchive keeps the accepted sources of a contest for the similarity check, under
// data/contests/<contest>/problem/<problem>/<solution>.<extension> like the previous judge.
type SourceArchive interface {
	Keep(submission domain.Submission, extension string) error
	// Requested lists the contests the admin asked to check.
	Requested() ([]int, error)
	// Sources groups the kept files of a contest by problem and extension: solution id to path.
	Sources(contestID int) ([]SourceGroup, error)
	// Finish removes the request of a contest.
	Finish(contestID int) error
}

// SourceGroup holds the files that can be compared with each other.
type SourceGroup struct {
	ProblemID int
	Files     map[int]string
}

// SimilarityTool compares source files of one language and reports every pair it scored.
// Match.A and Match.B are the paths it was given.
type SimilarityTool interface {
	Compare(files []string) ([]domain.Match, error)
}

// SimilarityStore is the part of the database the similarity check reads and writes.
type SimilarityStore interface {
	AcceptedInContest(contestID int) ([]domain.Accepted, error)
	// ReplaceSimilarity deletes what was stored for the contest and saves the new result.
	ReplaceSimilarity(contestID int, found []domain.Similarity) error
}
