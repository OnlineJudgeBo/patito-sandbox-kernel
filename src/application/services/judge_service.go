package services

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/domain"
)

const (
	compileTimeSec  = 30
	compileMemoryKB = 1024 * 1024
	compileOutputKB = 128 * 1024
	checkerTimeSec  = 10
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

// JudgeService compiles a submission and runs it against the tests of its problem. ICPC style, it
// stops at the first test that is not accepted and that test gives the verdict. A problem with
// score groups, or a contest ranked by points, runs every test and the submission gets a score.
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

	var check *checker
	if submission.SpecialJudge && !submission.Custom {
		source := submission.Files.Checker
		switch {
		case source.Source != "":
			if err := j.buildChecker(boxID, source); err != nil {
				return failure("El checker del problema no compila; revisa checker.log en los archivos del problema.")
			}
			check = &checker{binary: source.Binary, testlib: !source.CMS, cms: source.CMS}
		case submission.Files.SpecialJudge != "":
			check = &checker{binary: submission.Files.SpecialJudge}
		default:
			return failure("El problema usa juez especial pero no tiene checker.cpp ni el programa spj.")
		}
	}
	graderFile := ""
	if graders := submission.Files.Graders; len(graders) > 0 {
		index := -1
		if language.Grader != nil {
			index = slices.IndexFunc(graders, func(path string) bool { return filepath.Base(path) == language.Grader.File })
		}
		if index < 0 {
			return domain.Result{Verdict: domain.OJ_CE, CompileOutput: "Este problema se resuelve implementando una función y no admite este lenguaje."}
		}
		graderFile = language.Grader.File
		language.Compile = language.Grader.Compile
		if len(language.Grader.Run) > 0 {
			language.Run = language.Grader.Run
		}
		for _, file := range append([]string{graders[index]}, submission.Files.Headers...) {
			if err := copyFile(file, filepath.Join(artifacts, filepath.Base(file))); err != nil {
				return failure("No se pudo preparar el grader: " + err.Error())
			}
		}
	}

	if len(language.Compile) > 0 {
		if result, ok := j.compile(boxID, language, artifacts); !ok {
			return result
		}
	}
	// The grader source is secret: once built, the running submission must not find it in its
	// box. An interpreted grader has to stay, it is what runs.
	if graderFile != "" && !slices.Contains(language.Run, graderFile) {
		os.Remove(filepath.Join(artifacts, graderFile))
	}

	timeSec, memoryKB := j.limits(submission, language)
	result := domain.Result{Verdict: domain.OJ_AC}
	result.Scored = !submission.Custom && (submission.ScoreAll || len(submission.Files.Groups) > 0)
	output := ""
	for _, test := range tests {
		outcome, detail := j.runTest(boxID, language, artifacts, test, timeSec, memoryKB, submission.Custom, check)
		if outcome.Verdict == domain.OJ_AC || outcome.Verdict == domain.OJ_PE {
			outcome.Score = 1 // a whitespace-only difference still earns the test
		}
		result.Tests = append(result.Tests, outcome)
		result.TimeMs = max(result.TimeMs, outcome.TimeMs)
		result.MemoryKB = max(result.MemoryKB, outcome.MemoryKB)
		if outcome.Verdict == domain.OJ_AC {
			output = detail
			continue
		}
		// When every test runs, the highest verdict code is the one shown, like the previous judge.
		if outcome.Verdict > result.Verdict {
			result.Verdict, result.RuntimeOutput = outcome.Verdict, detail
		}
		if !result.Scored {
			break
		}
	}
	if result.Scored {
		result.Groups, result.Score = domain.Score(submission.Files.Groups, result.Tests)
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

func (j *JudgeService) runTest(boxID int, language domain.Language, artifacts string, test domain.TestCase, timeSec float64, memoryKB int, keepOutput bool, check *checker) (domain.TestResult, string) {
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
	if check != nil {
		// Like the previous judge: no output limit, the answer may not be unique.
		verdict, score, err := j.specialJudge(boxID, *check, test, userOutput)
		if err != nil {
			outcome.Verdict = domain.OJ_RE
			return outcome, "No se pudo ejecutar el juez especial: " + err.Error()
		}
		outcome.Verdict, outcome.Score = verdict, score
		if verdict != domain.OJ_AC {
			return outcome, "El problema admite varias respuestas: la respuesta correcta que se muestra es solo una de las válidas.\n\n" +
				differenceReport(test, userOutput)
		}
		return outcome, ""
	}
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

// checker is the program that decides whether an output is correct. Without testlib or cms it
// is the binary of the previous judge: "spj <input> <answer> <output>", exit code 0 accepts.
type checker struct {
	binary  string
	testlib bool
	cms     bool
}

// buildChecker compiles the checker in the sandbox the first time and again when its source
// changes. The binary is written under a private name and renamed, so a judge running next to
// this one never executes a half-written file.
func (j *JudgeService) buildChecker(boxID int, source domain.Checker) error {
	built, errBuilt := os.Stat(source.Binary)
	written, err := os.Stat(source.Source)
	if err != nil {
		return err
	}
	if errBuilt == nil && !built.ModTime().Before(written.ModTime()) {
		return nil
	}
	box, err := j.Sandbox.Init(boxID)
	if err != nil {
		return err
	}
	defer j.Sandbox.Cleanup(boxID)
	if err := copyFile(source.Source, filepath.Join(box, "checker.cpp")); err != nil {
		return err
	}
	if source.Header != "" {
		if err := copyFile(source.Header, filepath.Join(box, "testlib.h")); err != nil {
			return err
		}
	}
	report, err := j.Sandbox.Run(boxID, abstractions.RunRequest{
		Args:      []string{"/usr/bin/g++", "-O2", "-std=c++17", "-o", "checker", "checker.cpp"},
		Stdout:    "compile.txt",
		TimeSec:   compileTimeSec * 4,
		MemoryKB:  compileMemoryKB,
		OutputKB:  compileOutputKB,
		Processes: 128,
	})
	copyFile(filepath.Join(box, "compile.txt"), source.Log)
	if err != nil {
		return err
	}
	if report.Status != "" {
		return fmt.Errorf("checker does not compile: %s", report.Message)
	}
	private := fmt.Sprintf("%s.%d.%d", source.Binary, os.Getpid(), boxID)
	if err := copyFile(filepath.Join(box, "checker"), private); err != nil {
		return err
	}
	return os.Rename(private, source.Binary)
}

// specialJudge runs the checker in a box of its own and returns the verdict of the test and the
// share of it the output earned.
func (j *JudgeService) specialJudge(boxID int, check checker, test domain.TestCase, userOutput string) (int, float64, error) {
	held, err := os.CreateTemp("", "patito-output-")
	if err != nil {
		return 0, 0, err
	}
	held.Close()
	defer os.Remove(held.Name())
	if err := copyFile(userOutput, held.Name()); err != nil {
		return 0, 0, err
	}
	// The box of the program cannot be reused: its accounting would add up.
	j.Sandbox.Cleanup(boxID)
	box, err := j.Sandbox.Init(boxID)
	if err != nil {
		return 0, 0, err
	}
	for name, source := range map[string]string{"spj": check.binary, "data.in": test.Input, "answer.out": test.Output, "user.out": held.Name()} {
		if err := copyFile(source, filepath.Join(box, name)); err != nil {
			return 0, 0, err
		}
	}
	args := []string{"./spj", "data.in", "answer.out", "user.out"}
	if check.testlib {
		args = []string{"./spj", "data.in", "user.out", "answer.out"}
	}
	report, err := j.Sandbox.Run(boxID, abstractions.RunRequest{
		Args:      args,
		Stdout:    "spj.txt",
		Stderr:    "spj.err",
		TimeSec:   checkerTimeSec,
		MemoryKB:  compileMemoryKB,
		OutputKB:  runOutputKB,
		Processes: 1,
	})
	if err != nil {
		return 0, 0, err
	}
	if report.Status == "XX" {
		return 0, 0, fmt.Errorf("%s", report.Message)
	}
	accepted := report.Status == ""
	switch {
	case check.cms:
		// CMS: the outcome is the first thing printed.
		return partial(accepted, filepath.Join(box, "spj.txt"), "")
	case accepted || !check.testlib:
		break
	case report.Status != "RE":
		return 0, 0, fmt.Errorf("el checker terminó de forma inesperada: %s", report.Message)
	case report.ExitCode == 2: // _pe
		return domain.OJ_PE, 1, nil
	case report.ExitCode == 7: // quitp writes "points <fraction> <message>"
		return partial(true, filepath.Join(box, "spj.err"), "points")
	case report.ExitCode != 1: // _fail or a crash: not the fault of the submission
		return 0, 0, fmt.Errorf("el checker falló con código %d: %s", report.ExitCode, strings.TrimSpace(readLimited(filepath.Join(box, "spj.err"))))
	}
	if accepted {
		return domain.OJ_AC, 1, nil
	}
	return domain.OJ_WA, 0, nil
}

// partial reads the fraction a checker reported, after an optional word. A test is accepted only
// with the whole of it.
func partial(finished bool, path string, word string) (int, float64, error) {
	fields := strings.Fields(readLimited(path))
	if word != "" && len(fields) > 0 && fields[0] == word {
		fields = fields[1:]
	}
	if !finished || len(fields) == 0 {
		return 0, 0, fmt.Errorf("el checker no informó el puntaje del caso")
	}
	score, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("el checker informó un puntaje ilegible: %q", fields[0])
	}
	score = min(max(score, 0), 1)
	if score >= 1 {
		return domain.OJ_AC, 1, nil
	}
	return domain.OJ_WA, score, nil
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
