// Package judgedb reads submissions from, and writes verdicts to, the judge database.
// The queries mirror the previous judge: the web, the API and the IDE keep working unchanged.
package judgedb

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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

func (s *Store) Pending(limit int, languages []int) ([]int, error) {
	if len(languages) == 0 {
		return nil, nil
	}
	return s.ids("SELECT solution_id FROM solution WHERE result < 2 AND language IN ("+languageList(languages)+")"+
		" ORDER BY result ASC, solution_id ASC LIMIT ?", limit)
}

func languageList(languages []int) string {
	ids := make([]string, len(languages))
	for i, language := range languages {
		ids[i] = strconv.Itoa(language)
	}
	return strings.Join(ids, ",")
}

func (s *Store) ids(query string, args ...any) ([]int, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
		// A run without a problem gets the limits the previous judge gave it.
		"SELECT s.problem_id, s.user_id, s.language, s.contest_id, COALESCE(p.time_limit, 5), COALESCE(p.memory_limit, 128), c.source,"+
			// The admin API stores Y/N; older rows hold 1/0.
			" COALESCE(p.spj IN ('1', 'Y'), 0),"+
			" EXISTS (SELECT 1 FROM custom_input i WHERE i.solution_id = s.solution_id)"+
			" FROM solution s LEFT JOIN problem p ON p.problem_id = s.problem_id"+
			" JOIN source_code c ON c.solution_id = s.solution_id WHERE s.solution_id = ?", id).
		Scan(&submission.ProblemID, &submission.UserID, &submission.LanguageID, &contest,
			&submission.TimeLimitSec, &submission.MemoryLimitMB, &submission.Source, &submission.SpecialJudge, &submission.Custom)
	submission.ContestID = int(contest.Int64)
	if err != nil {
		return submission, err
	}
	if submission.ProblemID <= 0 {
		submission.Custom = true
	}
	if !submission.Custom {
		return submission, nil
	}

	rows, err := s.db.Query("SELECT input_text, expected_output FROM custom_input_case WHERE solution_id = ? ORDER BY case_number", id)
	if err != nil {
		return submission, err
	}
	defer rows.Close()
	for rows.Next() {
		var input string
		var expected sql.NullString
		if err := rows.Scan(&input, &expected); err != nil {
			return submission, err
		}
		item := domain.CustomCase{Input: input}
		if expected.Valid {
			item.Expected = &expected.String
		}
		submission.CustomCases = append(submission.CustomCases, item)
	}
	return submission, rows.Err()
}

func (s *Store) RecoverStuck(olderThan time.Duration) (int, error) {
	result, err := s.db.Exec(
		"UPDATE solution SET result = ? WHERE result IN (?, ?) AND judgetime < NOW() - INTERVAL ? SECOND",
		domain.OJ_WT1, domain.OJ_CI, domain.OJ_RI, int(olderThan.Seconds()))
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}

func (s *Store) AcceptedInContest(contestID int) ([]domain.Accepted, error) {
	rows, err := s.db.Query("SELECT solution_id, user_id, problem_id FROM solution WHERE contest_id = ? AND result = ?", contestID, domain.OJ_AC)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accepted []domain.Accepted
	for rows.Next() {
		var item domain.Accepted
		if err := rows.Scan(&item.ID, &item.UserID, &item.ProblemID); err != nil {
			return nil, err
		}
		accepted = append(accepted, item)
	}
	return accepted, rows.Err()
}

func (s *Store) SaveSimilarity(found domain.Similarity) error {
	_, err := s.db.Exec("INSERT INTO similar_code (solution_id, similar_s_id, percentage) VALUES (?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE percentage = VALUES(percentage)", found.SolutionID, found.SimilarID, found.Percentage)
	return err
}

func (s *Store) ReplaceSimilarity(contestID int, found []domain.Similarity) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE sc FROM similar_code sc JOIN solution s ON s.solution_id = sc.solution_id WHERE s.contest_id = ?", contestID); err != nil {
		return err
	}
	for _, item := range found {
		if _, err := tx.Exec("INSERT INTO similar_code (solution_id, similar_s_id, percentage) VALUES (?, ?, ?)",
			item.SolutionID, item.SimilarID, item.Percentage); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetRunning(id int) error {
	_, err := s.db.Exec("UPDATE solution SET result = ? WHERE solution_id = ?", domain.OJ_RI, id)
	return err
}

// ToUTF8 keeps the accents of tools that print Latin-1, like PSeInt; the database rejects other bytes.
func ToUTF8(text string) string {
	if utf8.ValidString(text) {
		return text
	}
	runes := make([]rune, len(text))
	for i := 0; i < len(text); i++ {
		runes[i] = rune(text[i])
	}
	return string(runes)
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
		if _, err := s.db.Exec("INSERT INTO "+table+" (solution_id, error) VALUES (?, ?)", submission.ID, ToUTF8(text)); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec("UPDATE solution SET result = ?, time = ?, memory = ? WHERE solution_id = ?",
		result.Verdict, result.TimeMs, result.MemoryKB, submission.ID); err != nil {
		return err
	}

	if submission.Custom {
		return nil // a run from the IDE is not a submission
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
