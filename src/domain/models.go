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
	// Checker is the source of a checker; it wins over SpecialJudge.
	Checker Checker
	// Graders own the entry point and are built with the submission, which only writes a
	// function. There is one per language the problem accepts: grader.cpp, grader.java, grader.py.
	// Headers are the *.h the C++ submission includes.
	Graders []string
	Headers []string
	// Groups come from scoring.json: the problem is scored by subtasks.
	Groups []ScoreGroup
}

// Checker is a checker given as source, built once and kept next to it. Source is empty when
// the problem has none.
type Checker struct {
	// Source is checker.cpp, which follows testlib: "checker <input> <output> <answer>" and the
	// exit code is the verdict. With CMS set it is checker_cms.cpp: "checker <input> <answer>
	// <output>" and it prints the outcome, a fraction from 0 to 1.
	Source string
	CMS    bool
	// Header is the testlib.h the problem brings, or empty to use the one of the judge.
	Header string
	// Binary and Log are where the judge leaves the built checker and the compiler output.
	Binary string
	Log    string
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
