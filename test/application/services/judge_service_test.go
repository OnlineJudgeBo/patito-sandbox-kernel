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

func TestGraderIsBuiltWithTheSubmissionInTheLanguagesTheProblemBrings(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "task.h")
	os.WriteFile(header, []byte("int solve();"), 0644)
	var graders []string
	for _, name := range []string{"grader.cpp", "grader.java", "grader.py"} {
		graders = append(graders, filepath.Join(dir, name))
		os.WriteFile(graders[len(graders)-1], []byte("main"), 0644)
	}
	sandbox := &fakeSandbox{t: t, run: echoInput}
	submission := cpp()
	submission.Files = domain.ProblemFiles{Graders: graders, Headers: []string{header}}
	tests := writeTests(t, [2]string{"1\n", "1\n"})
	// Language id, how it is built, how it runs, and whether the grader source is still there when running.
	cases := []struct {
		language int
		build    string
		run      string
		kept     string
	}{
		{1, "Main.cc grader.cpp", "./Main", ""},
		{3, "Main.java grader.java", "-Xss64m grader", ""},
		{6, "Main.py grader.py", "python3 grader.py", "grader.py"},
	}
	for _, item := range cases {
		sandbox.requests = nil
		sandbox.run = func(box string, request abstractions.RunRequest) abstractions.RunReport {
			for _, name := range []string{"grader.cpp", "grader.java", "grader.py"} {
				if !isCompile(request) && exists(filepath.Join(box, name)) != (name == item.kept) {
					t.Fatalf("language %d: %s in the box of the running submission", item.language, name)
				}
			}
			return echoInput(box, request)
		}
		submission.LanguageID = item.language

		result := judge(sandbox).Evaluate(0, submission, tests)

		build, run := strings.Join(sandbox.requests[0].Args, " "), strings.Join(sandbox.requests[1].Args, " ")
		if result.Verdict != domain.OJ_AC || !strings.HasSuffix(build, item.build) && !strings.Contains(build, item.build+" ") || !strings.HasSuffix(run, item.run) {
			t.Fatalf("language %d: verdict %d, built with %q, ran %q", item.language, result.Verdict, build, run)
		}
	}

	submission.LanguageID = 18
	if result := judge(sandbox).Evaluate(0, submission, tests); result.Verdict != domain.OJ_CE {
		t.Fatalf("a language without a grader cannot solve the problem: %+v", result)
	}
	submission.LanguageID, submission.Files.Graders = 3, graders[:1]
	if result := judge(sandbox).Evaluate(0, submission, tests); result.Verdict != domain.OJ_CE {
		t.Fatalf("the problem only brings the C++ grader: %+v", result)
	}
}

func TestScoreGroupsRunEveryTestAndPayEachGroupByItsType(t *testing.T) {
	sandbox := &fakeSandbox{t: t, run: echoInput}
	submission := cpp()
	submission.Files.Groups = []domain.ScoreGroup{
		{Points: 10, Type: "sum", Patterns: []string{"[ab]"}},
		{Points: 20, Type: "min", Patterns: []string{"b", "c"}},
		{Points: 30, Type: "mul", Patterns: []string{"c"}},
	}

	// a passes, b is wrong, c differs only in whitespace.
	result := judge(sandbox).Evaluate(0, submission, writeTests(t, [2]string{"1\n", "1\n"}, [2]string{"2\n", "5\n"}, [2]string{"3\n", "3 \n"}))

	if result.Verdict != domain.OJ_WA || len(result.Tests) != 3 || !strings.Contains(result.RuntimeOutput, "Respuesta Correcta:\n5") {
		t.Fatalf("every test must run and the worst one give the verdict: %+v", result)
	}
	want := []domain.GroupScore{{Points: 5, MaxPoints: 10, Tests: 2}, {Points: 0, MaxPoints: 20, Tests: 2}, {Points: 30, MaxPoints: 30, Tests: 1}}
	if !result.Scored || result.Score != 35.0/60 || len(result.Groups) != 3 || result.Groups[0] != want[0] || result.Groups[1] != want[1] || result.Groups[2] != want[2] {
		t.Fatalf("unexpected score: %v %+v", result.Score, result.Groups)
	}
}

func TestAFailedSampleStopsAScoredSubmissionWithNoPoints(t *testing.T) {
	sandbox := &fakeSandbox{t: t, run: echoInput}
	submission := cpp()
	submission.Files.Groups = []domain.ScoreGroup{{Points: 100, Type: "sum", Patterns: []string{"[ab]"}}}
	tests := writeTests(t, [2]string{"1\n", "1\n"}, [2]string{"2\n", "2\n"}, [2]string{"3\n", "5\n"})
	tests[2].Name = "sample"

	result := judge(sandbox).Evaluate(0, submission, tests)

	if result.Verdict != domain.OJ_WA || len(result.Tests) != 1 || !result.Scored || result.Score != 0 || result.Groups != nil {
		t.Fatalf("the sample must run first and stop the rest: %+v", result)
	}

	// A sample that passes does not change the points of the groups.
	os.WriteFile(tests[2].Output, []byte("3\n"), 0644)
	result = judge(sandbox).Evaluate(0, submission, tests)
	if result.Verdict != domain.OJ_AC || len(result.Tests) != 3 || result.Score != 1 {
		t.Fatalf("unexpected result with a passing sample: %+v", result)
	}
}

func TestAPointsContestScoresAProblemWithoutGroupsByAcceptedTests(t *testing.T) {
	sandbox := &fakeSandbox{t: t, run: echoInput}
	submission := cpp()
	submission.ScoreAll = true

	result := judge(sandbox).Evaluate(0, submission, writeTests(t, [2]string{"1\n", "1\n"}, [2]string{"2\n", "5\n"}, [2]string{"3\n", "3\n"}, [2]string{"4\n", "4\n"}))

	if result.Verdict != domain.OJ_WA || len(result.Tests) != 4 || result.Score != 0.75 || result.Groups != nil {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSourceCheckerIsBuiltOnceAndGivesVerdictsAndPartialCredit(t *testing.T) {
	dir := t.TempDir()
	source := domain.Checker{Source: filepath.Join(dir, "checker.cpp"), Header: filepath.Join(dir, "testlib.h"),
		Binary: filepath.Join(dir, "checker"), Log: filepath.Join(dir, "checker.log")}
	os.WriteFile(source.Source, []byte("checker"), 0644)
	os.WriteFile(source.Header, []byte("testlib"), 0644)
	builds, compiles := 0, true
	// What the checker answers for each test, by the content of its input.
	answers := map[string]abstractions.RunReport{}
	printed := map[string]string{}
	sandbox := &fakeSandbox{t: t}
	sandbox.run = func(box string, request abstractions.RunRequest) abstractions.RunReport {
		switch {
		case request.Args[len(request.Args)-1] == "checker.cpp":
			builds++
			if !exists(filepath.Join(box, "testlib.h")) {
				t.Fatal("the testlib.h of the problem must be next to the checker")
			}
			os.WriteFile(filepath.Join(box, request.Stdout), []byte("compiler says"), 0644)
			if !compiles {
				return abstractions.RunReport{Status: "RE", ExitCode: 1}
			}
			os.WriteFile(filepath.Join(box, "checker"), []byte("binary"), 0755)
			return abstractions.RunReport{}
		case request.Args[0] == "./spj":
			input, _ := os.ReadFile(filepath.Join(box, "data.in"))
			os.WriteFile(filepath.Join(box, request.Stdout), []byte(printed[string(input)]), 0644)
			os.WriteFile(filepath.Join(box, request.Stderr), []byte(printed[string(input)]), 0644)
			return answers[string(input)]
		}
		return echoInput(box, request)
	}
	submission := cpp()
	submission.SpecialJudge = true
	submission.Files.Checker = source
	submission.Files.Groups = []domain.ScoreGroup{{Points: 100, Type: "sum", Patterns: []string{"*"}}}
	tests := writeTests(t, [2]string{"ok", "x"}, [2]string{"wa", "x"}, [2]string{"pe", "x"}, [2]string{"half", "x"})

	compiles = false
	if result := judge(sandbox).Evaluate(0, submission, tests); result.Verdict != domain.OJ_RE || !strings.Contains(result.RuntimeOutput, "checker.log") {
		t.Fatalf("a checker that does not compile is not a wrong answer: %+v", result)
	}
	if log, _ := os.ReadFile(source.Log); string(log) != "compiler says" {
		t.Fatalf("the compiler output must be kept: %q", log)
	}

	// testlib: exit code 0 ok, 1 wrong, 2 presentation, 7 partial with "points <fraction>".
	compiles = true
	answers = map[string]abstractions.RunReport{"ok": {}, "wa": {Status: "RE", ExitCode: 1}, "pe": {Status: "RE", ExitCode: 2}, "half": {Status: "RE", ExitCode: 7}}
	printed = map[string]string{"half": "points 0.5 close enough"}
	result := judge(sandbox).Evaluate(0, submission, tests)
	if result.Verdict != domain.OJ_WA || result.Score != 0.625 || result.Tests[2].Verdict != domain.OJ_PE || result.Tests[3].Score != 0.5 {
		t.Fatalf("unexpected testlib result: %+v", result)
	}
	if args := sandbox.requests[len(sandbox.requests)-1].Args; args[2] != "user.out" || args[3] != "answer.out" {
		t.Fatalf("testlib takes the output before the answer: %v", args)
	}
	judge(sandbox).Evaluate(0, submission, tests)
	if builds != 2 {
		t.Fatalf("the checker must be built once and reused, built %d times", builds)
	}
	answers["wa"] = abstractions.RunReport{Status: "RE", ExitCode: 3}
	if result := judge(sandbox).Evaluate(0, submission, tests); result.Verdict != domain.OJ_RE {
		t.Fatalf("a failing checker is not the fault of the submission: %+v", result)
	}

	// CMS: the checker prints the outcome and takes the answer before the output.
	submission.Files.Checker.CMS = true
	answers = map[string]abstractions.RunReport{"ok": {}, "wa": {}, "pe": {}, "half": {}}
	printed = map[string]string{"ok": "1.0\n", "wa": "0\n", "pe": "1\n", "half": "0.25\n"}
	result = judge(sandbox).Evaluate(0, submission, tests)
	if result.Verdict != domain.OJ_WA || result.Score != 0.5625 || result.Tests[0].Verdict != domain.OJ_AC {
		t.Fatalf("unexpected CMS result: %+v", result)
	}
	if args := sandbox.requests[len(sandbox.requests)-1].Args; args[2] != "answer.out" || args[3] != "user.out" {
		t.Fatalf("a CMS checker takes the answer before the output: %v", args)
	}
}
