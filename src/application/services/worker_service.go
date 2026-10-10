package services

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/domain"
)

// Worker polls the database for pending submissions and judges up to Parallel of them at once.
type Worker struct {
	Store     abstractions.Store
	Tests     abstractions.TestCaseProvider
	Judge     *JudgeService
	Languages []int
	Parallel  int
	Sleep     time.Duration
	// Archive keeps accepted contest sources for the similarity check; nil turns it off.
	Archive abstractions.SourceArchive
	// StuckAfter is how long a claimed submission may go without a verdict before it is queued again.
	StuckAfter time.Duration
}

// Run returns when the context is cancelled and every submission in progress has a verdict.
func (w *Worker) Run(ctx context.Context) {
	boxes := make(chan int, w.Parallel)
	for box := 0; box < w.Parallel; box++ {
		boxes <- box
	}
	var running sync.WaitGroup
	defer running.Wait()

	var recovered time.Time
	for {
		if w.StuckAfter > 0 && time.Since(recovered) > time.Minute {
			recovered = time.Now()
			if count, err := w.Store.RecoverStuck(w.StuckAfter); err != nil {
				log.Printf("could not recover stuck submissions: %v", err)
			} else if count > 0 {
				log.Printf("queued again %d submission(s) left without a verdict", count)
			}
		}
		ids, err := w.Store.Pending(w.Parallel*2, w.Languages)
		if err != nil {
			log.Printf("could not read pending submissions: %v", err)
		}
		for _, id := range ids {
			var box int
			select {
			case box = <-boxes:
			case <-ctx.Done():
				return
			}
			claimed, err := w.Store.Claim(id)
			if err != nil || !claimed {
				boxes <- box
				if err != nil {
					log.Printf("could not claim submission %d: %v", id, err)
				}
				continue
			}
			running.Add(1)
			go func(id int, box int) {
				defer running.Done()
				defer func() { boxes <- box }()
				w.judge(id, box)
			}(id, box)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(w.Sleep):
		}
	}
}

func (w *Worker) judge(id int, box int) {
	submission, err := w.Store.Load(id)
	if err != nil {
		// Without the submission there is nothing to write a verdict for; it stays claimed in the log.
		log.Printf("could not load submission %d: %v", id, err)
		return
	}
	var result domain.Result
	var tests []domain.TestCase
	if submission.Custom {
		dir, errDir := os.MkdirTemp("", "patito-custom-")
		if errDir == nil {
			defer os.RemoveAll(dir)
			tests, err = CustomTests(dir, submission.CustomCases)
		} else {
			err = errDir
		}
	} else {
		tests, err = w.Tests.GetTestCases(submission.ProblemID)
	}
	if err != nil {
		result = failure("No se pudieron leer los casos de prueba: " + err.Error())
	} else {
		if err := w.Store.SetRunning(id); err != nil {
			log.Printf("could not mark submission %d as running: %v", id, err)
		}
		result = w.Judge.Evaluate(box, submission, tests)
	}
	if err := w.Store.Save(submission, result); err != nil {
		log.Printf("could not save the verdict of submission %d: %v", id, err)
		return
	}
	if w.Archive != nil && result.Verdict == domain.OJ_AC && submission.ContestID > 0 {
		if language, ok := domain.LanguageByID(submission.LanguageID, domain.JavaOptions{}); ok {
			if err := w.Archive.Keep(submission, strings.TrimPrefix(filepath.Ext(language.Source), ".")); err != nil {
				log.Printf("could not keep the source of submission %d: %v", id, err)
			}
		}
	}
	log.Printf("submission %d problem %d language %d: verdict %d, %d ms, %d KB, %d test(s)",
		id, submission.ProblemID, submission.LanguageID, result.Verdict, result.TimeMs, result.MemoryKB, len(result.Tests))
}
