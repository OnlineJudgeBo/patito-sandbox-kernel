package services_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/application/services"
	"github.com/samuelloza/isolate-wrapper/src/domain"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/comparator"
)

// fakeSandbox stands in for isolate: each Run is answered by the test, which may write files into the box.
type fakeSandbox struct {
	t        *testing.T
	box      string
	live     bool
	requests []abstractions.RunRequest
	run      func(box string, request abstractions.RunRequest) abstractions.RunReport
}

func (f *fakeSandbox) Init(int) (string, error) {
	if f.live {
		f.t.Fatal("a box was reused without Cleanup: its accounting would be cumulative")
	}
	f.live = true
	f.box = f.t.TempDir()
	return f.box, nil
}

func (f *fakeSandbox) Cleanup(int) error {
	f.live = false
	return nil
}

func (f *fakeSandbox) Run(_ int, request abstractions.RunRequest) (abstractions.RunReport, error) {
	f.requests = append(f.requests, request)
	return f.run(f.box, request), nil
}

func isCompile(request abstractions.RunRequest) bool {
	return request.Stdin == ""
}

// echoInput behaves like a correct program for tests whose answer equals their input.
func echoInput(box string, request abstractions.RunRequest) abstractions.RunReport {
	if isCompile(request) {
		os.WriteFile(filepath.Join(box, "Main"), []byte("binary"), 0755)
		return abstractions.RunReport{}
	}
	input, _ := os.ReadFile(filepath.Join(box, request.Stdin))
	os.WriteFile(filepath.Join(box, request.Stdout), input, 0644)
	return abstractions.RunReport{TimeSec: 0.25, MemoryKB: 2048}
}

func writeTests(t *testing.T, cases ...[2]string) []domain.TestCase {
	dir := t.TempDir()
	var tests []domain.TestCase
	for index, item := range cases {
		name := string(rune('a' + index))
		input, output := filepath.Join(dir, name+".in"), filepath.Join(dir, name+".out")
		os.WriteFile(input, []byte(item[0]), 0644)
		os.WriteFile(output, []byte(item[1]), 0644)
		tests = append(tests, domain.TestCase{Name: name, Input: input, Output: output})
	}
	return tests
}

func judge(sandbox *fakeSandbox) *services.JudgeService {
	return &services.JudgeService{
		Sandbox:    sandbox,
		Comparator: &comparator.Comparator{},
		Options: services.JudgeOptions{
			Java:          domain.JavaOptions{Xms: "-Xms64m", Xmx: "-Xmx128m"},
			BonusTimeSec:  2,
			BonusMemoryMB: 64,
		},
	}
}

func cpp() domain.Submission {
	return domain.Submission{ID: 7, ProblemID: 1000, LanguageID: 1, Source: "int main(){}", TimeLimitSec: 1, MemoryLimitMB: 64}
}

func TestAcceptedReportsTheSlowestTestAndPeakMemory(t *testing.T) {
	calls := 0
	sandbox := &fakeSandbox{t: t}
	sandbox.run = func(box string, request abstractions.RunRequest) abstractions.RunReport {
		report := echoInput(box, request)
		if !isCompile(request) {
			calls++
			report.TimeSec = 0.1 * float64(calls)
			report.MemoryKB = 1000 * calls
		}
		return report
	}

	result := judge(sandbox).Evaluate(0, cpp(), writeTests(t, [2]string{"1\n", "1\n"}, [2]string{"2\n", "2\n"}, [2]string{"3\n", "3\n"}))

	if result.Verdict != domain.OJ_AC || len(result.Tests) != 3 || result.TimeMs != 300 || result.MemoryKB != 3000 {
		t.Fatalf("unexpected result: %+v", result)
	}
	run := sandbox.requests[1]
	if run.TimeSec != 1 || run.MemoryKB != 64*1024 || run.Processes != 1 || run.Args[0] != "./Main" {
		t.Fatalf("a C++ run must use the problem limits untouched: %+v", run)
	}
}

func TestStopsAtTheFirstTestThatIsNotAccepted(t *testing.T) {
	sandbox := &fakeSandbox{t: t, run: echoInput}

	result := judge(sandbox).Evaluate(0, cpp(), writeTests(t, [2]string{"1\n", "1\n"}, [2]string{"2\n", "5\n"}, [2]string{"3\n", "3\n"}))

	if result.Verdict != domain.OJ_WA || len(result.Tests) != 2 {
		t.Fatalf("expected wrong answer on the second test: %+v", result)
	}
	want := "Entrada \n=================\n2\n\n=================\nRespuesta Correcta:\n5\n\n-----------------\nTu respuesta:\n2\n\n=================\n"
	if result.RuntimeOutput != want {
		t.Fatalf("the failed test must be reported: %q", result.RuntimeOutput)
	}
}

func TestWhitespaceDifferenceIsAPresentationError(t *testing.T) {
	sandbox := &fakeSandbox{t: t, run: echoInput}

	result := judge(sandbox).Evaluate(0, cpp(), writeTests(t, [2]string{"1 2\n", "1\n2\n"}))

	if result.Verdict != domain.OJ_PE {
		t.Fatalf("expected presentation error: %+v", result)
	}
}

func TestSandboxOutcomesBecomeVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		report  abstractions.RunReport
		stderr  string
		verdict int
		detail  string
	}{
		{"time limit", abstractions.RunReport{Status: "TO", TimeSec: 1.1}, "", domain.OJ_TL, ""},
		{"memory limit", abstractions.RunReport{Status: "SG", Signal: 9, OOMKilled: true}, "", domain.OJ_ML, ""},
		{"java heap", abstractions.RunReport{Status: "RE", ExitCode: 1}, "Exception in thread \"main\" java.lang.OutOfMemoryError: Java heap space", domain.OJ_ML, ""},
		{"output limit", abstractions.RunReport{Status: "SG", Signal: 25}, "", domain.OJ_OL, ""},
		{"crash", abstractions.RunReport{Status: "SG", Signal: 11, Message: "Caught fatal signal 11"}, "", domain.OJ_RE, "Caught fatal signal 11"},
		{"non-zero exit", abstractions.RunReport{Status: "RE", ExitCode: 3, Message: "Exited with error status 3"}, "boom", domain.OJ_RE, "boom"},
		{"sandbox failure", abstractions.RunReport{Status: "XX", Message: "cannot mount"}, "", domain.OJ_RE, "cannot mount"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			sandbox := &fakeSandbox{t: t}
			sandbox.run = func(box string, request abstractions.RunRequest) abstractions.RunReport {
				if isCompile(request) {
					return abstractions.RunReport{}
				}
				os.WriteFile(filepath.Join(box, request.Stderr), []byte(item.stderr), 0644)
				return item.report
			}

			result := judge(sandbox).Evaluate(0, cpp(), writeTests(t, [2]string{"1\n", "1\n"}))

			if result.Verdict != item.verdict || !strings.Contains(result.RuntimeOutput, item.detail) {
				t.Fatalf("got verdict %d with %q", result.Verdict, result.RuntimeOutput)
			}
			if item.verdict == domain.OJ_TL && result.TimeMs != 1000 {
				t.Fatalf("a time limit reports the limit itself, got %d ms", result.TimeMs)
			}
		})
	}
}

func TestAnOutputMuchLongerThanTheAnswerIsAnOutputLimit(t *testing.T) {
	sandbox := &fakeSandbox{t: t}
	sandbox.run = func(box string, request abstractions.RunRequest) abstractions.RunReport {
		if !isCompile(request) {
			os.WriteFile(filepath.Join(box, request.Stdout), []byte(strings.Repeat("x", 5000)), 0644)
		}
		return abstractions.RunReport{}
	}

	result := judge(sandbox).Evaluate(0, cpp(), writeTests(t, [2]string{"1\n", "1\n"}))

	if result.Verdict != domain.OJ_OL {
		t.Fatalf("expected output limit: %+v", result)
	}
}

func TestCompilationErrorKeepsTheCompilerOutputAndRunsNothing(t *testing.T) {
	sandbox := &fakeSandbox{t: t}
	sandbox.run = func(box string, request abstractions.RunRequest) abstractions.RunReport {
		os.WriteFile(filepath.Join(box, request.Stdout), []byte("Main.cc:1: error: expected ';'"), 0644)
		return abstractions.RunReport{Status: "RE", ExitCode: 1}
	}

	result := judge(sandbox).Evaluate(0, cpp(), writeTests(t, [2]string{"1\n", "1\n"}))

	if result.Verdict != domain.OJ_CE || !strings.Contains(result.CompileOutput, "expected ';'") || len(sandbox.requests) != 1 {
		t.Fatalf("unexpected result: %+v after %d runs", result, len(sandbox.requests))
	}
	compile := sandbox.requests[0]
	if compile.Stderr != "" || compile.Processes < 2 || compile.TimeSec < 10 {
		t.Fatalf("the compiler needs merged output, child processes and its own time limit: %+v", compile)
	}
}

func TestJavaGetsTheConfiguredBonusAndItsRuntimeFlags(t *testing.T) {
	sandbox := &fakeSandbox{t: t, run: echoInput}
	submission := cpp()
	submission.LanguageID = 3

	result := judge(sandbox).Evaluate(0, submission, writeTests(t, [2]string{"1\n", "1\n"}))

	run := sandbox.requests[1]
	if result.Verdict != domain.OJ_AC || run.TimeSec != 3 || run.MemoryKB != 128*1024 || run.Processes < 2 {
		t.Fatalf("unexpected java run: %+v (%+v)", run, result)
	}
	if strings.Join(run.Args, " ") != "/usr/bin/java -Xms64m -Xmx128m -XX:+UseSerialGC -Xss64m Main" {
		t.Fatalf("unexpected java command: %v", run.Args)
	}
}

func TestLimitsOutOfRangeAreClampedLikeThePreviousJudge(t *testing.T) {
	sandbox := &fakeSandbox{t: t, run: echoInput}
	submission := cpp()
	submission.TimeLimitSec, submission.MemoryLimitMB = 0, 5000

	judge(sandbox).Evaluate(0, submission, writeTests(t, [2]string{"1\n", "1\n"}))

	if run := sandbox.requests[1]; run.TimeSec != 300 || run.MemoryKB != 1024*1024 {
		t.Fatalf("unexpected limits: %+v", run)
	}
}

func TestSubmissionsThatCannotBeJudgedStillGetAVerdict(t *testing.T) {
	sandbox := &fakeSandbox{t: t, run: echoInput}
	unsupported := cpp()
	unsupported.LanguageID = 99

	if result := judge(sandbox).Evaluate(0, unsupported, writeTests(t, [2]string{"1\n", "1\n"})); result.Verdict != domain.OJ_CE {
		t.Fatalf("an unsupported language is a compilation error: %+v", result)
	}
	if result := judge(sandbox).Evaluate(0, cpp(), nil); result.Verdict != domain.OJ_RE || result.RuntimeOutput == "" {
		t.Fatalf("a problem without tests must not be accepted: %+v", result)
	}
	if len(sandbox.requests) != 0 {
		t.Fatalf("nothing should have run, got %d runs", len(sandbox.requests))
	}
}

func TestCustomRunShowsTheOutputAndIsNotASubmissionVerdict(t *testing.T) {
	sandbox := &fakeSandbox{t: t, run: echoInput}
	submission := cpp()
	submission.Custom = true
	expected, wrong := "2\n", "9\n"

	run := func(cases ...domain.CustomCase) domain.Result {
		tests, err := services.CustomTests(t.TempDir(), cases)
		if err != nil {
			t.Fatal(err)
		}
		return judge(sandbox).Evaluate(0, submission, tests)
	}

	if result := run(domain.CustomCase{Input: "1\n"}, domain.CustomCase{Input: "2\n", Expected: &expected}); result.Verdict != domain.OJ_TR || result.RuntimeOutput != "2\n" {
		t.Fatalf("a finished run reports the output of the last input: %+v", result)
	}
	if result := run(); result.Verdict != domain.OJ_TR || result.RuntimeOutput != "" {
		t.Fatalf("a run without inputs still runs once: %+v", result)
	}
	if result := run(domain.CustomCase{Input: "2\n", Expected: &wrong}); result.Verdict != domain.OJ_WA || !strings.Contains(result.RuntimeOutput, "Respuesta Correcta:\n9") {
		t.Fatalf("an expected output is compared: %+v", result)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestSpecialJudgeDecidesInsteadOfComparing(t *testing.T) {
	spj := filepath.Join(t.TempDir(), "spj")
	os.WriteFile(spj, []byte("checker"), 0755)
	accepts := true
	sandbox := &fakeSandbox{t: t}
	sandbox.run = func(box string, request abstractions.RunRequest) abstractions.RunReport {
		if request.Args[0] != "./spj" {
			return echoInput(box, request)
		}
		for _, name := range []string{"spj", "data.in", "answer.out", "user.out"} {
			if !exists(filepath.Join(box, name)) {
				t.Fatalf("the special judge needs %s in its box", name)
			}
		}
		if accepts {
			return abstractions.RunReport{}
		}
		return abstractions.RunReport{Status: "RE", ExitCode: 1}
	}
	submission := cpp()
	submission.SpecialJudge = true
	// The answer file differs from the output: only the special judge can accept it.
	tests := writeTests(t, [2]string{"1\n", "another valid answer\n"})

	if result := judge(sandbox).Evaluate(0, submission, tests); result.Verdict != domain.OJ_RE {
		t.Fatalf("a special judge problem without spj cannot be judged: %+v", result)
	}
	submission.Files.SpecialJudge = spj
	if result := judge(sandbox).Evaluate(0, submission, tests); result.Verdict != domain.OJ_AC {
		t.Fatalf("expected accepted: %+v", result)
	}
	accepts = false
	if result := judge(sandbox).Evaluate(0, submission, tests); result.Verdict != domain.OJ_WA || result.RuntimeOutput != "" {
		t.Fatalf("expected wrong answer without a difference report: %+v", result)
	}
}

func TestGraderIsCompiledWithTheSubmissionAndHiddenFromIt(t *testing.T) {
	dir := t.TempDir()
	grader, header := filepath.Join(dir, "grader.cpp"), filepath.Join(dir, "task.h")
	os.WriteFile(grader, []byte("int main(){}"), 0644)
	os.WriteFile(header, []byte("int solve();"), 0644)
	sandbox := &fakeSandbox{t: t}
	sandbox.run = func(box string, request abstractions.RunRequest) abstractions.RunReport {
		if isCompile(request) != exists(filepath.Join(box, "grader.cpp")) {
			t.Fatalf("grader.cpp must be in the box only while compiling (compile=%v)", isCompile(request))
		}
		return echoInput(box, request)
	}
	submission := cpp()
	submission.Files = domain.ProblemFiles{Grader: grader, Headers: []string{header}}
	tests := writeTests(t, [2]string{"1\n", "1\n"})

	result := judge(sandbox).Evaluate(0, submission, tests)

	if result.Verdict != domain.OJ_AC || !strings.Contains(strings.Join(sandbox.requests[0].Args, " "), "Main.cc grader.cpp") {
		t.Fatalf("unexpected grader build: %+v (%v)", result, sandbox.requests[0].Args)
	}
	submission.LanguageID = 3
	if result := judge(sandbox).Evaluate(0, submission, tests); result.Verdict != domain.OJ_CE {
		t.Fatalf("graders are C++ only: %+v", result)
	}
}
