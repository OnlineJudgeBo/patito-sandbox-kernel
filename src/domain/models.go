package domain

type EvaluationInput struct {
	ID         string              `json:"id"`
	UniqID     string              `json:"uniqId"`
	BoxID      int                 `json:"boxId"`
	ProblemID  int                 `json:"problemID"`
	Language   string              `json:"language"`
	SourceCode string              `json:"sourceCode"`
	RunLimits  RunLimits           `json:"runLimits"`
	TestCases  []TestCase          `json:"testCases"`
	SQL        *SQLEvaluationInput `json:"sql,omitempty"`
}

// SQLEvaluationInput describes trusted fixtures and expected tabular results
// for a SQL submission. Schema and Seed are supplied by the judge backend;
// SourceCode remains the untrusted query submitted by the contestant.
type SQLEvaluationInput struct {
	Dialect      string        `json:"dialect"`
	OrderMatters bool          `json:"orderMatters"`
	MaxRows      int           `json:"maxRows"`
	TestCases    []SQLTestCase `json:"testCases"`
}

type SQLTestCase struct {
	Schema   string       `json:"schema"`
	Seed     string       `json:"seed"`
	Expected SQLResultSet `json:"expected"`
}

type SQLResultSet struct {
	Columns []string    `json:"columns"`
	Rows    [][]*string `json:"rows"`
}

type RunLimits struct {
	Time   int `json:"time"`
	Memory int `json:"memory"`
	Output int `json:"output"`
}

type TestCase struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

type SandboxLog struct {
	ExecutionTime int
	MemoryUsed    int
}
