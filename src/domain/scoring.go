package domain

import "path"

// ScoreGroup is a subtask declared in the scoring.json of a problem.
type ScoreGroup struct {
	Points float64
	// Type says how the group pays: "sum" in proportion to the tests passed, "min" the worst
	// test (all or nothing), "mul" the product of its tests.
	Type string
	// Patterns are globs over the test name; a test may belong to several groups.
	Patterns []string
}

// GroupScore is what a submission earned in one group.
type GroupScore struct {
	Points    float64
	MaxPoints float64
	Tests     int
}

// Score returns the points of every group and the total as a fraction from 0 to 1. Without
// groups the fraction is the share of accepted tests. Inside a group every test counts by its
// Score, and a test outside every group earns nothing.
func Score(groups []ScoreGroup, tests []TestResult) ([]GroupScore, float64) {
	if len(groups) == 0 {
		accepted := 0
		for _, test := range tests {
			if test.Verdict == OJ_AC {
				accepted++
			}
		}
		if len(tests) == 0 {
			return nil, 0
		}
		return nil, float64(accepted) / float64(len(tests))
	}

	scores := make([]GroupScore, len(groups))
	earned, total := 0.0, 0.0
	for index, group := range groups {
		sum, lowest, product, count := 0.0, 1.0, 1.0, 0
		for _, test := range tests {
			if !group.has(test.Name) {
				continue
			}
			count++
			sum += test.Score
			lowest = min(lowest, test.Score)
			product *= test.Score
		}
		factor := 0.0
		switch {
		case count == 0:
		case group.Type == "min":
			factor = lowest
		case group.Type == "mul":
			factor = product
		default:
			factor = sum / float64(count)
		}
		scores[index] = GroupScore{Points: group.Points * factor, MaxPoints: group.Points, Tests: count}
		earned += scores[index].Points
		total += group.Points
	}
	if total == 0 {
		return scores, 0
	}
	return scores, earned / total
}

func (g ScoreGroup) has(test string) bool {
	for _, pattern := range g.Patterns {
		if matched, _ := path.Match(pattern, test); matched {
			return true
		}
	}
	return false
}
