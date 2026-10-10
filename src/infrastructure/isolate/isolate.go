package isolate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
)

const binary = "/usr/local/bin/isolate"

// IsolateSandbox runs programs with ioi/isolate using control groups, so memory is accounted for
// the whole process tree and not per process.
type IsolateSandbox struct {
	// MetaDir holds one meta file per box; it must not be visible from inside the boxes.
	MetaDir string
}

func (s *IsolateSandbox) Init(boxID int) (string, error) {
	// A box left behind by a crash would make --init fail.
	exec.Command(binary, "--cg", fmt.Sprintf("--box-id=%d", boxID), "--cleanup").Run()
	output, err := exec.Command(binary, "--cg", fmt.Sprintf("--box-id=%d", boxID), "--init").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("isolate --init: %s", strings.TrimSpace(string(output)))
	}
	return filepath.Join(strings.TrimSpace(string(output)), "box"), nil
}

func (s *IsolateSandbox) Cleanup(boxID int) error {
	return exec.Command(binary, "--cg", fmt.Sprintf("--box-id=%d", boxID), "--cleanup").Run()
}

func (s *IsolateSandbox) Run(boxID int, request abstractions.RunRequest) (abstractions.RunReport, error) {
	meta := filepath.Join(s.MetaDir, fmt.Sprintf("meta-%d", boxID))
	os.Remove(meta)
	args := BuildOptions(boxID, meta, request)

	output, runErr := exec.Command(binary, args...).CombinedOutput()
	data, err := os.ReadFile(meta)
	if err != nil {
		return abstractions.RunReport{}, fmt.Errorf("isolate left no meta file (%v): %s", runErr, strings.TrimSpace(string(output)))
	}
	return ParseMeta(string(data)), nil
}

// BuildOptions is exported for the tests: the limits a submission gets are decided here.
func BuildOptions(boxID int, meta string, request abstractions.RunRequest) []string {
	args := []string{
		"--cg",
		fmt.Sprintf("--box-id=%d", boxID),
		"--meta=" + meta,
		fmt.Sprintf("--cg-mem=%d", request.MemoryKB),
		fmt.Sprintf("--time=%.3f", request.TimeSec),
		// Wall time catches programs that sleep or block instead of burning CPU.
		fmt.Sprintf("--wall-time=%.3f", request.TimeSec*3+5),
		"--extra-time=0.5",
		fmt.Sprintf("--fsize=%d", request.OutputKB),
		fmt.Sprintf("--processes=%d", max(request.Processes, 1)),
		// Compilers and runtimes need their configuration and a search path.
		"--dir=/etc",
		"--env=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"--env=HOME=/box",
		"--env=LANG=C.UTF-8",
	}
	if request.Stdin != "" {
		args = append(args, "--stdin="+request.Stdin)
	}
	if request.Stdout != "" {
		args = append(args, "--stdout="+request.Stdout)
	}
	if request.Stderr != "" {
		args = append(args, "--stderr="+request.Stderr)
	} else {
		args = append(args, "--stderr-to-stdout")
	}
	args = append(args, "--run", "--")
	return append(args, request.Args...)
}

// ParseMeta reads the "key:value" lines isolate writes after a run.
func ParseMeta(text string) abstractions.RunReport {
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if key, value, found := strings.Cut(line, ":"); found {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	report := abstractions.RunReport{
		Status:    values["status"],
		OOMKilled: values["cg-oom-killed"] == "1",
		Message:   values["message"],
	}
	report.ExitCode, _ = strconv.Atoi(values["exitcode"])
	report.Signal, _ = strconv.Atoi(values["exitsig"])
	report.TimeSec, _ = strconv.ParseFloat(values["time"], 64)
	report.MemoryKB, _ = strconv.Atoi(values["cg-mem"])
	return report
}
