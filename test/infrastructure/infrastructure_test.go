package infrastructure_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/config"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/isolate"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/judgedb"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/testcaseprovider"
)

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

func TestConfigReadsTheSameFileAsThePreviousJudge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "judge.conf")
	os.WriteFile(path, []byte("OJ_HOST_NAME=db\nOJ_USER_NAME = judge\nOJ_PASSWORD=p=w\nOJ_DB_NAME=jol\nOJ_PORT_NUMBER=3307\n"+
		"OJ_RUNNING=4\nOJ_SLEEP_TIME=2\nOJ_JAVA_TIME_BONUS=2\nOJ_JAVA_MEMORY_BONUS=64\nOJ_JAVA_XMX=-Xmx256m\nOJ_LANG_SET=0,1, 3,19\nOJ_SIM_ENABLE=1\n"), 0600)

	settings, err := config.Load(path)

	if err != nil || settings.DBHost != "db" || settings.DBUser != "judge" || settings.DBPassword != "p=w" || settings.DBPort != 3307 {
		t.Fatalf("unexpected database settings: %+v (%v)", settings, err)
	}
	if settings.Parallel != 4 || settings.SleepSeconds != 2 || settings.BonusTimeSec != 2 || settings.BonusMemoryMB != 64 || settings.JavaXmx != "-Xmx256m" || settings.JavaXms != "-Xms64m" {
		t.Fatalf("unexpected judge settings: %+v", settings)
	}
	if !reflect.DeepEqual(settings.Languages, []int{0, 1, 3, 19}) {
		t.Fatalf("unexpected languages: %v", settings.Languages)
	}
	if _, err := config.Load(filepath.Join(t.TempDir(), "missing.conf")); err == nil {
		t.Fatal("a missing file must be an error")
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
