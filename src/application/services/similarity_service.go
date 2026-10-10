package services

import (
	"context"
	"log"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/domain"
)

// Solutions at least this similar are reported, the same cut the previous judge used.
const similarityThreshold = 0.7

// SimilarityService compares accepted contest solutions with the ones of the other participants,
// never with their author's own: each one when it is accepted, and the whole contest again when
// the admin asks for it.
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

// CheckSubmission keeps the source of an accepted contest submission and stores its closest
// match among the accepted solutions other users sent to the same problem in the same language.
func (s *SimilarityService) CheckSubmission(submission domain.Submission) error {
	language, ok := domain.LanguageByID(submission.LanguageID, domain.JavaOptions{})
	if !ok {
		return nil
	}
	if err := s.Archive.Keep(submission, strings.TrimPrefix(filepath.Ext(language.Source), ".")); err != nil {
		return err
	}
	accepted, err := s.Store.AcceptedInContest(submission.ContestID)
	if err != nil {
		return err
	}
	author := map[int]domain.Accepted{}
	for _, item := range accepted {
		author[item.ID] = item
	}
	groups, err := s.Archive.Sources(submission.ContestID)
	if err != nil {
		return err
	}
	for _, group := range groups {
		own, mine := group.Files[submission.ID]
		if !mine || group.ProblemID != submission.ProblemID {
			continue
		}
		files := []string{own}
		solution := map[string]int{}
		for id, path := range group.Files {
			if item, ok := author[id]; ok && item.ProblemID == group.ProblemID && item.UserID != submission.UserID {
				files = append(files, path)
				solution[path] = id
			}
		}
		if len(files) < 2 {
			return nil
		}
		sort.Strings(files)
		matches, err := s.Tool.Compare(files)
		if err != nil {
			return err
		}
		best := domain.Similarity{SolutionID: submission.ID}
		top := 0.0
		for _, match := range matches {
			other := solution[match.A] + solution[match.B] // the own file is not in the map
			if (match.A == own || match.B == own) && other > 0 && match.Score >= similarityThreshold && match.Score > top {
				top = match.Score
				best.SimilarID, best.Percentage = other, int(math.Round(match.Score*100))
			}
		}
		if top == 0 {
			return nil
		}
		return s.Store.SaveSimilarity(best)
	}
	return nil
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
