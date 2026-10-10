package abstractions

// RunRequest is one execution inside a sandbox box. File names are relative to the box directory.
type RunRequest struct {
	Args   []string
	Stdin  string
	Stdout string
	// Stderr is a file name, or empty to merge it into Stdout.
	Stderr    string
	TimeSec   float64
	MemoryKB  int
	OutputKB  int
	Processes int
}

// RunReport is what the sandbox measured.
type RunReport struct {
	// Status is empty when the program exited with code 0. Otherwise:
	// "TO" timed out, "SG" killed by a signal, "RE" non-zero exit code, "XX" sandbox failure.
	Status    string
	ExitCode  int
	Signal    int
	OOMKilled bool
	TimeSec   float64
	MemoryKB  int
	Message   string
}

// Sandbox isolates one execution. A box must not be reused between executions: its resource
// accounting is cumulative, so every Run gets its own Init and Cleanup.
type Sandbox interface {
	// Init creates the box and returns the host path of its working directory.
	Init(boxID int) (string, error)
	Run(boxID int, request RunRequest) (RunReport, error)
	Cleanup(boxID int) error
}
