package services_test

import (
	"reflect"
	"testing"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/application/services"
	"github.com/samuelloza/isolate-wrapper/src/domain"
)

type fakeSimilarity struct {
	accepted []domain.Accepted
	groups   []abstractions.SourceGroup
	matches  []domain.Match
	compared [][]string
	saved    []domain.Similarity
	kept     []string
}

func (f *fakeSimilarity) SaveSimilarity(found domain.Similarity) error {
	f.saved = append(f.saved, found)
	return nil
}

func (f *fakeSimilarity) AcceptedInContest(int) ([]domain.Accepted, error) { return f.accepted, nil }
func (f *fakeSimilarity) ReplaceSimilarity(_ int, found []domain.Similarity) error {
	f.saved = found
	return nil
}
func (f *fakeSimilarity) Keep(_ domain.Submission, extension string) error {
	f.kept = append(f.kept, extension)
	return nil
}
func (f *fakeSimilarity) Requested() ([]int, error)                       { return nil, nil }
func (f *fakeSimilarity) Finish(int) error                                { return nil }
func (f *fakeSimilarity) Sources(int) ([]abstractions.SourceGroup, error) { return f.groups, nil }
func (f *fakeSimilarity) Compare(files []string) ([]domain.Match, error) {
	f.compared = append(f.compared, files)
	return f.matches, nil
}

func TestSimilarityComparesWithOtherUsersOnly(t *testing.T) {
	fake := &fakeSimilarity{
		accepted: []domain.Accepted{{ID: 1, UserID: "ana", ProblemID: 10}, {ID: 2, UserID: "ana", ProblemID: 10}, {ID: 3, UserID: "beto", ProblemID: 10}, {ID: 4, UserID: "caro", ProblemID: 10}},
		// 5 is no longer accepted: its file stays on disk but must not be compared.
		groups: []abstractions.SourceGroup{
			{ProblemID: 10, Files: map[int]string{1: "1.cc", 2: "2.cc", 3: "3.cc", 4: "4.cc", 5: "5.cc"}},
			{ProblemID: 11, Files: map[int]string{1: "other/1.cc"}},
		},
		matches: []domain.Match{
			{A: "1.cc", B: "2.cc", Score: 1},     // same author
			{A: "1.cc", B: "3.cc", Score: 0.914}, // reported for both
			{A: "3.cc", B: "4.cc", Score: 0.95},  // closer match of 3
			{A: "1.cc", B: "4.cc", Score: 0.4},   // below the cut
		},
	}
	service := &services.SimilarityService{Store: fake, Archive: fake, Tool: fake}

	found, err := service.CheckContest(7)

	want := []domain.Similarity{{SolutionID: 1, SimilarID: 3, Percentage: 91}, {SolutionID: 3, SimilarID: 4, Percentage: 95}, {SolutionID: 4, SimilarID: 3, Percentage: 95}}
	if err != nil || found != 3 || !reflect.DeepEqual(fake.saved, want) {
		t.Fatalf("unexpected result: %+v (%v)", fake.saved, err)
	}
	if !reflect.DeepEqual(fake.compared, [][]string{{"1.cc", "2.cc", "3.cc", "4.cc"}}) {
		t.Fatalf("unexpected files compared: %v", fake.compared)
	}
}

func TestAnAcceptedSubmissionIsComparedWithTheOtherUsers(t *testing.T) {
	fake := &fakeSimilarity{
		accepted: []domain.Accepted{{ID: 1, UserID: "ana", ProblemID: 10}, {ID: 2, UserID: "ana", ProblemID: 10}, {ID: 3, UserID: "beto", ProblemID: 10}, {ID: 4, UserID: "caro", ProblemID: 10}},
		groups: []abstractions.SourceGroup{
			{ProblemID: 10, Files: map[int]string{9: "9.py"}},
			{ProblemID: 10, Files: map[int]string{1: "1.cc", 2: "2.cc", 3: "3.cc", 4: "4.cc", 5: "5.cc"}},
		},
		matches: []domain.Match{
			{A: "2.cc", B: "3.cc", Score: 0.8},
			{A: "4.cc", B: "2.cc", Score: 0.93},
			{A: "3.cc", B: "4.cc", Score: 1}, // not about this submission
		},
	}
	service := &services.SimilarityService{Store: fake, Archive: fake, Tool: fake}

	err := service.CheckSubmission(domain.Submission{ID: 2, UserID: "ana", ProblemID: 10, ContestID: 7, LanguageID: 16})

	if err != nil || !reflect.DeepEqual(fake.saved, []domain.Similarity{{SolutionID: 2, SimilarID: 4, Percentage: 93}}) {
		t.Fatalf("unexpected result: %+v (%v)", fake.saved, err)
	}
	// Her other solution (1) and the one no longer accepted (5) stay out.
	if !reflect.DeepEqual(fake.compared, [][]string{{"2.cc", "3.cc", "4.cc"}}) || !reflect.DeepEqual(fake.kept, []string{"cc"}) {
		t.Fatalf("unexpected files: compared %v, kept %v", fake.compared, fake.kept)
	}
}
