// Command judge is the Patito judge daemon: it takes pending submissions from the database,
// runs them inside isolate and writes the verdict back.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/application/services"
	"github.com/samuelloza/isolate-wrapper/src/domain"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/comparator"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/config"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/dolos"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/isolate"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/judgedb"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/sourcearchive"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/testcaseprovider"
)

func env(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func main() {
	check := flag.Bool("check", false, "run a program inside the sandbox and exit: tells whether this host can judge")
	flag.Parse()

	metaDir, err := os.MkdirTemp("", "patito-judge-meta-")
	if err != nil {
		log.Fatalf("meta directory: %v", err)
	}
	defer os.RemoveAll(metaDir)
	sandbox := &isolate.IsolateSandbox{MetaDir: metaDir}
	if *check {
		if err := selfCheck(sandbox); err != nil {
			log.Fatalf("this host cannot judge: %v", err)
		}
		log.Println("sandbox ok: this host can judge")
		return
	}

	home := env("JUDGE_HOME", "/home/judge")
	settings, err := config.Load(env("CONF", filepath.Join(home, "etc", "judge.json")))
	if err != nil {
		log.Fatalf("configuration: %v", err)
	}
	database, err := judgedb.Open(settings.DBHost, settings.DBPort, settings.DBUser, settings.DBPassword, settings.DBName)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer database.Close()

	worker := services.Worker{
		Store: database,
		Tests: testcaseprovider.NewFileSystemTestCaseProvider(filepath.Join(home, "data")),
		Judge: &services.JudgeService{
			Sandbox:    sandbox,
			Comparator: &comparator.Comparator{},
			Options: services.JudgeOptions{
				Java:          domain.JavaOptions{Xms: settings.JavaXms, Xmx: settings.JavaXmx},
				BonusTimeSec:  settings.BonusTimeSec,
				BonusMemoryMB: settings.BonusMemoryMB,
			},
		},
		Languages:  languages(settings.Languages),
		Parallel:   settings.Parallel,
		Sleep:      time.Duration(settings.SleepSeconds) * time.Second,
		StuckAfter: time.Duration(settings.StuckMinutes) * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	mode := "judging"
	switch {
	case settings.Shadow:
		// Shadow mode only compares verdicts: it must not touch submissions, counters or the archive.
		shadow, err := judgedb.NewShadowStore(database)
		if err != nil {
			log.Fatalf("shadow table: %v", err)
		}
		worker.Store, mode = shadow, "shadow"
	case settings.Similarity:
		archive := &sourcearchive.Archive{Root: filepath.Join(home, "data", "contests")}
		worker.Archive = archive
		similarity := &services.SimilarityService{Store: database, Archive: archive, Tool: &dolos.Tool{Timeout: 10 * time.Minute}}
		go similarity.Watch(ctx, 5*time.Second)
	}
	log.Printf("patito judge started (%s): %d parallel, languages %v, data in %s", mode, worker.Parallel, worker.Languages, filepath.Join(home, "data"))
	worker.Run(ctx)
	log.Println("patito judge stopped")
}

// selfCheck proves the sandbox works here: control groups, namespaces and limits.
func selfCheck(sandbox *isolate.IsolateSandbox) error {
	if _, err := sandbox.Init(0); err != nil {
		return err
	}
	defer sandbox.Cleanup(0)
	report, err := sandbox.Run(0, abstractions.RunRequest{
		Args: []string{"/bin/true"}, Stdout: "out.txt", TimeSec: 5, MemoryKB: 64 * 1024, OutputKB: 1024, Processes: 1,
	})
	if err != nil {
		return err
	}
	if report.Status != "" {
		return fmt.Errorf("isolate reported %s: %s", report.Status, report.Message)
	}
	return nil
}

// languages keeps the configured ids this judge can actually build and run.
func languages(configured []int) []int {
	supported := domain.SupportedLanguageIDs()
	if len(configured) == 0 {
		return supported
	}
	var enabled []int
	for _, id := range supported {
		for _, allowed := range configured {
			if id == allowed {
				enabled = append(enabled, id)
			}
		}
	}
	return enabled
}
