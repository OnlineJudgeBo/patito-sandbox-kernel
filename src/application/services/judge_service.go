package services

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/domain"
)

const (
	compileTimeSec  = 30
	compileMemoryKB = 1024 * 1024
	compileOutputKB = 128 * 1024
	// A program may write this much before it is cut; the real output limit is checked against the answer.
	runOutputKB    = 64 * 1024
	maxReportBytes = 8 * 1024
	// Limits a problem may ask for, the same clamp the previous judge applied.
	maxTimeSec  = 300
	maxMemoryMB = 1024
)

// JudgeOptions come from judge.json.
type JudgeOptions struct {
	Java domain.JavaOptions
	// Extra time (seconds) and memory (MB) for JVM and interpreted languages.
	BonusTimeSec  int
	BonusMemoryMB int
}

// JudgeService compiles a submission and runs it against the tests of its problem, ICPC style:
// it stops at the first test that is not accepted and that test gives the verdict.
type JudgeService struct {
	Sandbox    abstractions.Sandbox
	Comparator abstractions.Comparator
	Options    JudgeOptions
}

// Evaluate never returns an error: whatever goes wrong becomes a verdict, so a submission is
// never left without one.
func (j *JudgeService) Evaluate(boxID int, submission domain.Submission, tests []domain.TestCase) domain.Result {
	language, supported := domain.LanguageByID(submission.LanguageID, j.Options.Java)
	if !supported {
		return domain.Result{Verdict: domain.OJ_CE, CompileOutput: "Lenguaje no soportado por este juez."}
	}
	if len(tests) == 0 {
		return failure("El problema no tiene casos de prueba.")
	}

	// Build products live outside the boxes: every execution gets a fresh box.
	artifacts, err := os.MkdirTemp("", fmt.Sprintf("patito-judge-%d-", submission.ID))
	if err != nil {
		return failure("No se pudo preparar el directorio de trabajo: " + err.Error())
	}
	defer os.RemoveAll(artifacts)
	if err := os.WriteFile(filepath.Join(artifacts, language.Source), []byte(submission.Source), 0644); err != nil {
		return failure("No se pudo escribir el código fuente: " + err.Error())
	}

	if len(language.Compile) > 0 {
		if result, ok := j.compile(boxID, language, artifacts); !ok {
			return result
		}
	}

	timeSec, memoryKB := j.limits(submission, language)
	result := domain.Result{Verdict: domain.OJ_AC}
	output := ""
	for _, test := range tests {
		outcome, detail := j.runTest(boxID, language, artifacts, test, timeSec, memoryKB, submission.Custom)
		result.Tests = append(result.Tests, outcome)
		result.TimeMs = max(result.TimeMs, outcome.TimeMs)
		result.MemoryKB = max(result.MemoryKB, outcome.MemoryKB)
		if outcome.Verdict != domain.OJ_AC {
			result.Verdict = outcome.Verdict
			result.RuntimeOutput = detail
			break
		}
		output = detail
	}
	if submission.Custom && result.Verdict == domain.OJ_AC {
		// The IDE shows what the program printed for the last input.
		result.Verdict, result.RuntimeOutput = domain.OJ_TR, output
	}
	return result
}

func (j *JudgeService) limits(submission domain.Submission, language domain.Language) (float64, int) {
	timeSec, memoryMB := submission.TimeLimitSec, submission.MemoryLimitMB
	if language.Bonus {
		timeSec += j.Options.BonusTimeSec
		memoryMB += j.Options.BonusMemoryMB
	}
	if timeSec < 1 || timeSec > maxTimeSec {
		timeSec = maxTimeSec
	}
	if memoryMB < 1 || memoryMB > maxMemoryMB {
		memoryMB = maxMemoryMB
	}
	return float64(timeSec), memoryMB * 1024
}

// compile builds inside the sandbox, so a hostile source cannot reach the host through the compiler.
func (j *JudgeService) compile(boxID int, language domain.Language, artifacts string) (domain.Result, bool) {
	box, err := j.Sandbox.Init(boxID)
	if err != nil {
		return failure("No se pudo crear el sandbox: " + err.Error()), false
	}
	defer j.Sandbox.Cleanup(boxID)
	if err := copyDir(artifacts, box); err != nil {
		return failure("No se pudo copiar el código al sandbox: " + err.Error()), false
	}

	report, err := j.Sandbox.Run(boxID, abstractions.RunRequest{
		Args:      language.Compile,
		Stdout:    "compile.txt",
		TimeSec:   compileTimeSec,
		MemoryKB:  compileMemoryKB,
		OutputKB:  compileOutputKB,
		Processes: 128,
	})
	if err != nil || report.Status == "XX" {
		return failure("No se pudo ejecutar el compilador: " + report.Message + errorText(err)), false
	}
	if report.Status != "" {
		output := readLimited(filepath.Join(box, "compile.txt"))
		if report.Status == "TO" {
			output = "La compilación superó el tiempo permitido.\n" + output
		}
		return domain.Result{Verdict: domain.OJ_CE, CompileOutput: output}, false
	}

	os.Remove(filepath.Join(box, "compile.txt"))
	if err := copyDir(box, artifacts); err != nil {
		return failure("No se pudo recuperar el programa compilado: " + err.Error()), false
	}
	return domain.Result{}, true
}

func (j *JudgeService) runTest(boxID int, language domain.Language, artifacts string, test domain.TestCase, timeSec float64, memoryKB int, keepOutput bool) (domain.TestResult, string) {
	outcome := domain.TestResult{Name: test.Name, Verdict: domain.OJ_RE}
	box, err := j.Sandbox.Init(boxID)
	if err != nil {
		return outcome, "No se pudo crear el sandbox: " + err.Error()
	}
	defer j.Sandbox.Cleanup(boxID)
	if err := copyDir(artifacts, box); err != nil {
		return outcome, "No se pudo copiar el programa al sandbox: " + err.Error()
	}
	if err := copyFile(test.Input, filepath.Join(box, "data.in")); err != nil {
		return outcome, "No se pudo leer el caso de prueba: " + err.Error()
	}

	report, err := j.Sandbox.Run(boxID, abstractions.RunRequest{
		Args:      language.Run,
		Stdin:     "data.in",
		Stdout:    "user.out",
		Stderr:    "error.out",
		TimeSec:   timeSec,
		MemoryKB:  memoryKB,
		OutputKB:  runOutputKB,
		Processes: language.Processes,
	})
	if err != nil {
		return outcome, "No se pudo ejecutar el programa: " + err.Error()
	}
	outcome.TimeMs = int(report.TimeSec * 1000)
	outcome.MemoryKB = report.MemoryKB
	stderr := readLimited(filepath.Join(box, "error.out"))

	switch {
	case report.Status == "XX":
		return outcome, "Error interno del sandbox: " + report.Message
	case report.Status == "TO":
		outcome.Verdict = domain.OJ_TL
		outcome.TimeMs = int(timeSec * 1000)
		return outcome, ""
	case report.OOMKilled || strings.Contains(stderr, "java.lang.OutOfMemoryError") || strings.Contains(stderr, "MemoryError"):
		outcome.Verdict = domain.OJ_ML
		return outcome, ""
	case report.Status == "SG" && report.Signal == 25: // SIGXFSZ: wrote past the file size limit
		outcome.Verdict = domain.OJ_OL
		return outcome, ""
	case report.Status != "":
		return outcome, strings.TrimSpace(report.Message + "\n" + stderr)
	}

	userOutput := filepath.Join(box, "user.out")
	outcome.Verdict = domain.OJ_AC
	if test.Output != "" {
		if tooLong(test.Output, userOutput) {
			outcome.Verdict = domain.OJ_OL
			return outcome, ""
		}
		verdict, err := j.Comparator.Compare(test.Output, userOutput)
		if err != nil {
			outcome.Verdict = domain.OJ_RE
			return outcome, "No se pudo comparar la salida: " + err.Error()
		}
		outcome.Verdict = verdict
	}
	switch {
	case outcome.Verdict != domain.OJ_AC:
		return outcome, differenceReport(test, userOutput)
	case keepOutput:
		return outcome, readLimited(userOutput)
	}
	return outcome, ""
}

// CustomTests writes the inputs of a custom run as test files. A run without inputs gets an empty one.
func CustomTests(dir string, cases []domain.CustomCase) ([]domain.TestCase, error) {
	if len(cases) == 0 {
		cases = []domain.CustomCase{{}}
	}
	var tests []domain.TestCase
	for index, item := range cases {
		test := domain.TestCase{Name: fmt.Sprint(index + 1), Input: filepath.Join(dir, fmt.Sprintf("%d.in", index+1))}
		if err := os.WriteFile(test.Input, []byte(item.Input), 0644); err != nil {
			return nil, err
		}
		if item.Expected != nil {
			test.Output = filepath.Join(dir, fmt.Sprintf("%d.out", index+1))
			if err := os.WriteFile(test.Output, []byte(*item.Expected), 0644); err != nil {
				return nil, err
			}
		}
		tests = append(tests, test)
	}
	return tests, nil
}

// differenceReport shows the failed test like the previous judge did: input, answer and output, each cut short.
func differenceReport(test domain.TestCase, userOutput string) string {
	return "Entrada \n=================\n" + excerpt(test.Input, 512) +
		"\n=================\nRespuesta Correcta:\n" + excerpt(test.Output, 900) +
		"\n-----------------\nTu respuesta:\n" + excerpt(userOutput, 900) +
		"\n=================\n"
}

func excerpt(path string, limit int64) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	data, _ := io.ReadAll(io.LimitReader(file, limit+1))
	if int64(len(data)) > limit {
		return string(data[:limit]) + "\n...\n"
	}
	return string(data)
}

// An output several times longer than the answer is an output limit, not a wrong answer.
func tooLong(expectedPath string, outputPath string) bool {
	expected, errExpected := os.Stat(expectedPath)
	output, errOutput := os.Stat(outputPath)
	return errExpected == nil && errOutput == nil && output.Size() > expected.Size()*4+1024
}

func failure(message string) domain.Result {
	return domain.Result{Verdict: domain.OJ_RE, RuntimeOutput: message}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return " " + err.Error()
}

func readLimited(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	data, _ := io.ReadAll(io.LimitReader(file, maxReportBytes))
	return string(data)
}

// copyDir copies the regular files and sub-directories of src into dst, readable by the box user.
func copyDir(src string, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !entry.Type().IsRegular() {
			return nil // never follow what a compiler run may have left behind
		}
		return copyFile(path, target)
	})
}

func copyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
