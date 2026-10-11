package domain

// Submission is one row of the solution table together with what is needed to judge it.
type Submission struct {
	ID         int
	ProblemID  int
	ContestID  int
	UserID     string
	LanguageID int
	Source     string
	// Limits of the problem, before the language bonus.
	TimeLimitSec  int
	MemoryLimitMB int
	// Custom marks a run from the IDE: it uses CustomCases instead of the tests of the problem,
	// reports the program output and does not count as a submission.
	Custom      bool
	CustomCases []CustomCase
	// SpecialJudge is the flag of the problem: answers are checked by a program, not compared.
	SpecialJudge bool
	// ScoreAll is set by a contest ranked by points: every test runs and the submission gets a score.
	ScoreAll bool
	// Files are the judge files of the problem, filled before judging.
	Files ProblemFiles
}

// ProblemFiles are the optional files that sit next to the tests of a problem. Empty means absent.
type ProblemFiles struct {
	// SpecialJudge is the binary "spj": it gets the input, the answer and the output of the
	// program as arguments and exits with 0 when the output is correct.
	SpecialJudge string
	// Grader owns main() and is compiled with the submission, which only writes a function.
	Grader  string
	Headers []string
	// Groups come from scoring.json: the problem is scored by subtasks.
	Groups []ScoreGroup
}

// CustomCase is one input typed in the IDE. Expected is nil when the output is only shown, not compared.
type CustomCase struct {
	Input    string
	Expected *string
}

// Accepted is an accepted contest solution, as the similarity check sees it.
type Accepted struct {
	ID        int
	UserID    string
	ProblemID int
}

// Match is one pair of source files a similarity tool compared; Score goes from 0 to 1.
type Match struct {
	A     string
	B     string
	Score float64
}

// Similarity is the closest solution of another user, kept when it is similar enough.
type Similarity struct {
	SolutionID int
	SimilarID  int
	Percentage int
}

// TestCase points to the input and expected output files of one test.
type TestCase struct {
	Name  string
	Input string
	// Output is empty when there is no answer to compare with (custom runs).
	Output string
}
