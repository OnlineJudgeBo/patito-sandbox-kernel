package services

import (
	"context"
	"log"
	"math"
	"sort"
	"time"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/domain"
)

// Solutions at least this similar are reported, the same cut the previous judge used.
const similarityThreshold = 0.7

// SimilarityService checks a whole contest when the admin asks for it: every accepted solution
// is compared with the ones of the other participants, never with its author's own.
type SimilarityService struct {
	Store   abstractions.SimilarityStore
	Archive abstractions.SourceArchive
	Tool    abstractions.SimilarityTool
}

// Watch serves the requests until the context is cancelled.
func (s *SimilarityService) Watch(ctx context.Context, every time.Duration) {
	for {
		contests, err := s.Archive.Requested()
		if err != nil {
			log.Printf("could not read the similarity requests: %v", err)
		}
		for _, contest := range contests {
			found, err := s.CheckContest(contest)
			if err != nil {
				log.Printf("similarity check of contest %d failed: %v", contest, err)
			} else {
				log.Printf("similarity check of contest %d: %d similar solution(s)", contest, found)
			}
			// The request is removed even after a failure, so a broken contest is not retried forever.
			if err := s.Archive.Finish(contest); err != nil {
				log.Printf("could not remove the similarity request of contest %d: %v", contest, err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// CheckContest replaces the stored result of the contest and returns how many solutions it flagged.
func (s *SimilarityService) CheckContest(contestID int) (int, error) {
	accepted, err := s.Store.AcceptedInContest(contestID)
	if err != nil {
		return 0, err
	}
	author := map[int]domain.Accepted{}
	for _, item := range accepted {
		author[item.ID] = item
	}
	groups, err := s.Archive.Sources(contestID)
	if err != nil {
		return 0, err
	}

	best := map[int]domain.Similarity{}
	score := map[int]float64{}
	for _, group := range groups {
		// Only solutions that are still accepted in this problem take part.
		var files []string
		solution := map[string]int{}
		for id, path := range group.Files {
			if item, ok := author[id]; ok && item.ProblemID == group.ProblemID {
				files = append(files, path)
				solution[path] = id
			}
		}
		if len(files) < 2 {
			continue
		}
		sort.Strings(files)
		matches, err := s.Tool.Compare(files)
		if err != nil {
			// A language the tool cannot read must not hide the result of the others.
			log.Printf("similarity check of contest %d problem %d skipped: %v", contestID, group.ProblemID, err)
			continue
		}
		for _, match := range matches {
			a, b := solution[match.A], solution[match.B]
			if a == 0 || b == 0 || author[a].UserID == author[b].UserID || match.Score < similarityThreshold {
				continue
			}
			for _, pair := range [][2]int{{a, b}, {b, a}} {
				if match.Score > score[pair[0]] {
					score[pair[0]] = match.Score
					best[pair[0]] = domain.Similarity{SolutionID: pair[0], SimilarID: pair[1], Percentage: int(math.Round(match.Score * 100))}
				}
			}
		}
	}

	found := make([]domain.Similarity, 0, len(best))
	for _, item := range best {
		found = append(found, item)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].SolutionID < found[j].SolutionID })
	return len(found), s.Store.ReplaceSimilarity(contestID, found)
}
