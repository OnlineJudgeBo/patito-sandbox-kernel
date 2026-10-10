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
}

// TestCase points to the input and expected output files of one test.
type TestCase struct {
	Name   string
	Input  string
	Output string
}
