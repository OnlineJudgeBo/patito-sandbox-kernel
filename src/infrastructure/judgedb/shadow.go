package judgedb

import (
	"time"

	"github.com/samuelloza/isolate-wrapper/src/domain"
)

// ShadowStore judges again what the previous judge already judged and keeps both verdicts side by
// side in shadow_verdict. It never changes a submission, so it can run next to the judge in use.
type ShadowStore struct {
	*Store
}

func NewShadowStore(store *Store) (*ShadowStore, error) {
	_, err := store.db.Exec("CREATE TABLE IF NOT EXISTS shadow_verdict (" +
		"solution_id INT NOT NULL PRIMARY KEY, legacy_result SMALLINT NOT NULL, legacy_time INT NOT NULL, legacy_memory INT NOT NULL," +
		" result SMALLINT NULL, time INT NULL, memory INT NULL, detail TEXT NULL, judged_at DATETIME NULL)")
	return &ShadowStore{Store: store}, err
}

// Pending returns judged submissions not compared yet, newest first.
func (s *ShadowStore) Pending(limit int, languages []int) ([]int, error) {
	if len(languages) == 0 {
		return nil, nil
	}
	return s.ids("SELECT s.solution_id FROM solution s LEFT JOIN shadow_verdict v ON v.solution_id = s.solution_id"+
		" WHERE v.solution_id IS NULL AND s.result BETWEEN ? AND ? AND s.problem_id > 0 AND s.language IN ("+languageList(languages)+")"+
		" AND NOT EXISTS (SELECT 1 FROM custom_input c WHERE c.solution_id = s.solution_id)"+
		" ORDER BY s.solution_id DESC LIMIT ?", domain.OJ_AC, domain.OJ_CE, limit)
}

// Claim records the verdict of the previous judge; the primary key lets only one judge take the row.
func (s *ShadowStore) Claim(id int) (bool, error) {
	result, err := s.db.Exec("INSERT IGNORE INTO shadow_verdict (solution_id, legacy_result, legacy_time, legacy_memory)"+
		" SELECT solution_id, result, time, memory FROM solution WHERE solution_id = ?", id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (s *ShadowStore) SetRunning(int) error {
	return nil
}

func (s *ShadowStore) Save(submission domain.Submission, result domain.Result) error {
	_, err := s.db.Exec("UPDATE shadow_verdict SET result = ?, time = ?, memory = ?, detail = ?, judged_at = NOW() WHERE solution_id = ?",
		result.Verdict, result.TimeMs, result.MemoryKB, ToUTF8(result.CompileOutput+result.RuntimeOutput), submission.ID)
	return err
}

func (s *ShadowStore) RecoverStuck(time.Duration) (int, error) {
	return 0, nil
}
