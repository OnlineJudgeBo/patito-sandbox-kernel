package infrastructure_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/domain"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/comparator"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/config"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/dolos"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/isolate"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/judgedb"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/testcaseprovider"
)

func TestWindowsLineEndingsInTheAnswerAreStillAccepted(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	answer := write("test.out", "8: -1\r\n19: 0 5\r\n")
	cases := map[string]int{"8: -1\n19: 0 5\n": domain.OJ_AC, "8: -1\n19:  0 5\n": domain.OJ_PE, "8: -1\n19: 0 6\n": domain.OJ_WA}
	for output, want := range cases {
		got, err := (&comparator.Comparator{}).Compare(answer, write("user.out", output))
		if err != nil || got != want {
			t.Errorf("%q: got %d, want %d (%v)", output, got, want, err)
		}
	}
}

func TestParseMetaReadsWhatIsolateReports(t *testing.T) {
	oom := isolate.ParseMeta("time:0.052\ntime-wall:0.069\nmax-rss:65864\ncg-mem:65536\ncg-oom-killed:1\nexitsig:9\nstatus:SG\nmessage:Caught fatal signal 9\n")
	if oom.Status != "SG" || oom.Signal != 9 || !oom.OOMKilled || oom.TimeSec != 0.052 || oom.MemoryKB != 65536 || oom.Message != "Caught fatal signal 9" {
		t.Fatalf("unexpected report: %+v", oom)
	}
	ok := isolate.ParseMeta("time:0.001\ncg-mem:336\nexitcode:0\n")
	if ok.Status != "" || ok.ExitCode != 0 || ok.OOMKilled {
		t.Fatalf("unexpected report: %+v", ok)
	}
}

func TestBuildOptionsAppliesEveryLimit(t *testing.T) {
	args := strings.Join(isolate.BuildOptions(3, "/meta/3", abstractions.RunRequest{
		Args: []string{"./Main"}, Stdin: "data.in", Stdout: "user.out", Stderr: "error.out",
		TimeSec: 2, MemoryKB: 65536, OutputKB: 1024, Processes: 1,
	}), " ")
	for _, expected := range []string{"--cg", "--box-id=3", "--meta=/meta/3", "--cg-mem=65536", "--time=2.000", "--fsize=1024", "--processes=1",
		"--stdin=data.in", "--stdout=user.out", "--stderr=error.out", "--run -- ./Main"} {
		if !strings.Contains(args, expected) {
			t.Fatalf("missing %q in: %s", expected, args)
		}
	}
	merged := strings.Join(isolate.BuildOptions(0, "/m", abstractions.RunRequest{Args: []string{"gcc"}, Stdout: "compile.txt"}), " ")
	if !strings.Contains(merged, "--stderr-to-stdout") || strings.Contains(merged, "--stdin=") {
		t.Fatalf("a compiler run merges its output and reads nothing: %s", merged)
	}
}

func TestConfigReadsJudgeJSON(t *testing.T) {
	write := func(content string) string {
		path := filepath.Join(t.TempDir(), "judge.json")
		os.WriteFile(path, []byte(content), 0600)
		return path
	}

	settings, err := config.Load(write(`{"dbHost":"db","dbUser":"judge","dbPassword":"p=w","dbName":"jol","dbPort":3307,
		"parallel":4,"sleepSeconds":2,"bonusTimeSeconds":2,"bonusMemoryMB":64,"javaXmx":"-Xmx256m","languages":[0,1,3,19]}`))

	if err != nil || settings.DBHost != "db" || settings.DBUser != "judge" || settings.DBPassword != "p=w" || settings.DBPort != 3307 {
		t.Fatalf("unexpected database settings: %+v (%v)", settings, err)
	}
	if settings.Parallel != 4 || settings.SleepSeconds != 2 || settings.BonusTimeSec != 2 || settings.BonusMemoryMB != 64 || settings.JavaXmx != "-Xmx256m" || settings.JavaXms != "-Xms64m" {
		t.Fatalf("unexpected judge settings: %+v", settings)
	}
	if !reflect.DeepEqual(settings.Languages, []int{0, 1, 3, 19}) {
		t.Fatalf("unexpected languages: %v", settings.Languages)
	}
	if settings, err := config.Load(write(`{"dbHost":"db","dbUser":"judge","dbName":"jol"}`)); err != nil || settings.DBPort != 3306 || settings.Parallel != 1 {
		t.Fatalf("unexpected defaults: %+v (%v)", settings, err)
	}
	for name, path := range map[string]string{
		"a missing file":     filepath.Join(t.TempDir(), "missing.json"),
		"a misspelled key":   write(`{"dbHost":"db","dbUser":"judge","dbName":"jol","paralel":4}`),
		"a missing database": write(`{"dbHost":"db"}`),
	} {
		if _, err := config.Load(path); err == nil {
			t.Fatalf("%s must be an error", name)
		}
	}
}

func TestTestCasesArePairedByNameAndSorted(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "1000")
	os.MkdirAll(filepath.Join(dir, "ac"), 0755)
	for _, name := range []string{"b.in", "b.out", "a.in", "a.out", "orphan.in", "sample.in.bak", "checker.cpp"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644)
	}

	tests, err := testcaseprovider.NewFileSystemTestCaseProvider(root).GetTestCases(1000)

	if err != nil || len(tests) != 2 || tests[0].Name != "a" || tests[1].Name != "b" || tests[0].Output != filepath.Join(dir, "a.out") {
		t.Fatalf("unexpected tests: %+v (%v)", tests, err)
	}
	if _, err := testcaseprovider.NewFileSystemTestCaseProvider(root).GetTestCases(404); err == nil {
		t.Fatal("a problem without a data folder must be an error")
	}
}

func TestCompilerOutputInLatin1IsStoredAsUTF8(t *testing.T) {
	if got := judgedb.ToUTF8("instrucci\xf3n"); got != "instrucción" {
		t.Fatalf("got %q", got)
	}
	if got := judgedb.ToUTF8("instrucción"); got != "instrucción" {
		t.Fatalf("valid text must not change: %q", got)
	}
}

func TestDolosPairsAreReadFromTheCSVReport(t *testing.T) {
	report := "id,leftFileId,leftFilePath,rightFileId,rightFilePath,similarity,totalOverlap\n15,0,4.cc,1,5.cc,0.91,26\n16,0,4.cc,2,other.cc,1,26\n"

	matches, err := dolos.ParsePairs(report, map[string]string{"4.cc": "/data/4.cc", "5.cc": "/data/5.cc"})

	if err != nil || !reflect.DeepEqual(matches, []domain.Match{{A: "/data/4.cc", B: "/data/5.cc", Score: 0.91}}) {
		t.Fatalf("unexpected matches: %+v (%v)", matches, err)
	}
	if _, err := dolos.ParsePairs("a,b\n1,2\n", nil); err == nil {
		t.Fatal("a report without the expected columns must be an error")
	}
}

func TestScoringFileGivesTheGroupsAndSkipsTheMalformedOnes(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "1000"), 0755)
	os.WriteFile(filepath.Join(root, "1000", "scoring.json"), []byte(`{"groups": [
		{"name": "Subtarea 1", "points": 10, "tests": "s1_*"},
		{"points": 20, "type": "min", "tests": ["s2_*", "extra"]},
		{"points": "many", "tests": "x"},
		{"points": 5},
		{"points": 5, "type": "other", "tests": ["y"]}]}`), 0644)

	groups := testcaseprovider.NewFileSystemTestCaseProvider(root).Files(1000).Groups

	if len(groups) != 3 || groups[0].Type != "sum" || groups[0].Patterns[0] != "s1_*" || groups[1].Type != "min" ||
		len(groups[1].Patterns) != 2 || groups[1].Points != 20 || groups[2].Type != "sum" {
		t.Fatalf("unexpected groups: %+v", groups)
	}
	if testcaseprovider.ParseGroups([]byte("not json")) != nil {
		t.Fatal("a file that is not JSON gives no groups")
	}
}
