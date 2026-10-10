// Package judgedb reads submissions from, and writes verdicts to, the judge database.
// The queries mirror the previous judge: the web, the API and the IDE keep working unchanged.
package judgedb

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/samuelloza/isolate-wrapper/src/domain"
)

type Store struct {
	db *sql.DB
}

func Open(host string, port int, user string, password string, name string) (*Store, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true", user, password, host, port, name)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, db.Ping()
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Pending skips runs with a custom input (the IDE's "run" button): this judge does not handle them yet.
func (s *Store) Pending(limit int, languages []int) ([]int, error) {
	if len(languages) == 0 {
		return nil, nil
	}
	ids := make([]string, len(languages))
	for i, language := range languages {
		ids[i] = strconv.Itoa(language)
	}
	rows, err := s.db.Query(
		"SELECT s.solution_id FROM solution s"+
			" WHERE s.result < 2 AND s.problem_id > 0 AND s.language IN ("+strings.Join(ids, ",")+")"+
			" AND NOT EXISTS (SELECT 1 FROM custom_input c WHERE c.solution_id = s.solution_id)"+
			" ORDER BY s.result ASC, s.solution_id ASC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pending []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		pending = append(pending, id)
	}
	return pending, rows.Err()
}

// Claim is atomic: of several judges looking at the same row, only one gets it.
func (s *Store) Claim(id int) (bool, error) {
	result, err := s.db.Exec(
		"UPDATE solution SET result = ?, time = 0, memory = 0, judgetime = NOW() WHERE solution_id = ? AND result < 2",
		domain.OJ_CI, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (s *Store) Load(id int) (domain.Submission, error) {
	submission := domain.Submission{ID: id}
	var contest sql.NullInt64
	err := s.db.QueryRow(
		"SELECT s.problem_id, s.user_id, s.language, s.contest_id, p.time_limit, p.memory_limit, c.source"+
			" FROM solution s JOIN problem p ON p.problem_id = s.problem_id"+
			" JOIN source_code c ON c.solution_id = s.solution_id WHERE s.solution_id = ?", id).
		Scan(&submission.ProblemID, &submission.UserID, &submission.LanguageID, &contest,
			&submission.TimeLimitSec, &submission.MemoryLimitMB, &submission.Source)
	submission.ContestID = int(contest.Int64)
	return submission, err
}

func (s *Store) SetRunning(id int) error {
	_, err := s.db.Exec("UPDATE solution SET result = ? WHERE solution_id = ?", domain.OJ_RI, id)
	return err
}

// Save writes the verdict last, after its details: whoever polls the row sees a finished submission.
func (s *Store) Save(submission domain.Submission, result domain.Result) error {
	for table, text := range map[string]string{"compileinfo": result.CompileOutput, "runtimeinfo": result.RuntimeOutput} {
		if _, err := s.db.Exec("DELETE FROM "+table+" WHERE solution_id = ?", submission.ID); err != nil {
			return err
		}
		if text == "" {
			continue
		}
		if _, err := s.db.Exec("INSERT INTO "+table+" (solution_id, error) VALUES (?, ?)", submission.ID, text); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec("UPDATE solution SET result = ?, time = ?, memory = ? WHERE solution_id = ?",
		result.Verdict, result.TimeMs, result.MemoryKB, submission.ID); err != nil {
		return err
	}

	// Counters shown in profiles and problem lists. A failure here does not undo the verdict.
	counters := []struct {
		query string
		args  []any
	}{
		{"UPDATE user_activity SET solved = (SELECT COUNT(DISTINCT problem_id) FROM solution WHERE user_id = ? AND result = 4) WHERE user_id = ?",
			[]any{submission.UserID, submission.UserID}},
		{"UPDATE user_activity SET submit = (SELECT COUNT(*) FROM solution WHERE user_id = ?) WHERE user_id = ?",
			[]any{submission.UserID, submission.UserID}},
		{"UPDATE problem SET accepted = (SELECT COUNT(*) FROM solution WHERE problem_id = ? AND result = 4) WHERE problem_id = ?",
			[]any{submission.ProblemID, submission.ProblemID}},
		{"UPDATE problem SET submit = (SELECT COUNT(*) FROM solution WHERE problem_id = ?) WHERE problem_id = ?",
			[]any{submission.ProblemID, submission.ProblemID}},
	}
	for _, counter := range counters {
		s.db.Exec(counter.query, counter.args...)
	}
	return nil
}
