// Command judge is the Patito judge daemon: it takes pending submissions from the database,
// runs them inside isolate and writes the verdict back.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/samuelloza/isolate-wrapper/src/application/services"
	"github.com/samuelloza/isolate-wrapper/src/domain"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/comparator"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/config"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/isolate"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/judgedb"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/testcaseprovider"
)

func env(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func main() {
	home := env("JUDGE_HOME", "/home/judge")
	settings, err := config.Load(env("CONF", filepath.Join(home, "etc", "judge.conf")))
	if err != nil {
		log.Fatalf("configuration: %v", err)
	}

	store, err := judgedb.Open(settings.DBHost, settings.DBPort, settings.DBUser, settings.DBPassword, settings.DBName)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer store.Close()

	metaDir, err := os.MkdirTemp("", "patito-judge-meta-")
	if err != nil {
		log.Fatalf("meta directory: %v", err)
	}
	defer os.RemoveAll(metaDir)

	worker := services.Worker{
		Store: store,
		Tests: testcaseprovider.NewFileSystemTestCaseProvider(filepath.Join(home, "data")),
		Judge: &services.JudgeService{
			Sandbox:    &isolate.IsolateSandbox{MetaDir: metaDir},
			Comparator: &comparator.Comparator{},
			Options: services.JudgeOptions{
				Java:          domain.JavaOptions{Xms: settings.JavaXms, Xmx: settings.JavaXmx},
				BonusTimeSec:  settings.BonusTimeSec,
				BonusMemoryMB: settings.BonusMemoryMB,
			},
		},
		Languages: languages(settings.Languages),
		Parallel:  settings.Parallel,
		Sleep:     time.Duration(settings.SleepSeconds) * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	log.Printf("patito judge started: %d parallel, languages %v, data in %s", worker.Parallel, worker.Languages, filepath.Join(home, "data"))
	worker.Run(ctx)
	log.Println("patito judge stopped")
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
