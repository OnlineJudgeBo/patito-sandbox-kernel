package services

import (
	"context"
	"log"
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
}

// Run returns when the context is cancelled and every submission in progress has a verdict.
func (w *Worker) Run(ctx context.Context) {
	boxes := make(chan int, w.Parallel)
	for box := 0; box < w.Parallel; box++ {
		boxes <- box
	}
	var running sync.WaitGroup
	defer running.Wait()

	for {
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
	tests, err := w.Tests.GetTestCases(submission.ProblemID)
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
	log.Printf("submission %d problem %d language %d: verdict %d, %d ms, %d KB, %d test(s)",
		id, submission.ProblemID, submission.LanguageID, result.Verdict, result.TimeMs, result.MemoryKB, len(result.Tests))
}
