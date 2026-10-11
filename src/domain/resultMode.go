package domain

// Verdict codes stored in solution.result. They are shared with the web, the API and the IDE.
const (
	OJ_WT0 = 0  // pending
	OJ_WT1 = 1  // pending rejudge
	OJ_CI  = 2  // compiling
	OJ_RI  = 3  // running
	OJ_AC  = 4  // accepted
	OJ_PE  = 5  // presentation error
	OJ_WA  = 6  // wrong answer
	OJ_TL  = 7  // time limit exceeded
	OJ_ML  = 8  // memory limit exceeded
	OJ_OL  = 9  // output limit exceeded
	OJ_RE  = 10 // runtime error
	OJ_CE  = 11 // compilation error
	OJ_TR  = 13 // run with a custom input finished
)

// TestResult is the outcome of one test case.
type TestResult struct {
	Name     string
	Verdict  int
	TimeMs   int
	MemoryKB int
}

// Result is what the judge writes back for a submission.
type Result struct {
	Verdict  int
	TimeMs   int
	MemoryKB int
	// Compiler output, kept when the verdict is OJ_CE.
	CompileOutput string
	// Why the program failed, kept when the verdict is OJ_RE.
	RuntimeOutput string
	Tests         []TestResult
	// Scored marks a submission judged on every test: Score is its fraction of the points,
	// from 0 to 1, and Groups what it earned in each group of the problem.
	Scored bool
	Score  float64
	Groups []GroupScore
}
