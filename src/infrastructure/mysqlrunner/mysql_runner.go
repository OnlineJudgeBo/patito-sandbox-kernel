package mysqlrunner

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/samuelloza/isolate-wrapper/src/domain"
)

const (
	defaultMaxRows = 1000
	maxAllowedRows = 10000
)

type Runner struct {
	adminConfig mysql.Config
}

func New(adminDSN string) (*Runner, error) {
	if strings.TrimSpace(adminDSN) == "" {
		return nil, errors.New("MYSQL_SANDBOX_ADMIN_DSN is required")
	}
	config, err := mysql.ParseDSN(adminDSN)
	if err != nil {
		return nil, fmt.Errorf("invalid MySQL sandbox DSN: %w", err)
	}
	config.MultiStatements = true
	config.ParseTime = true
	return &Runner{adminConfig: *config}, nil
}

func (r *Runner) Evaluate(ctx context.Context, input domain.EvaluationInput) (domain.EvaluationResult, error) {
	result := domain.EvaluationResult{SubmitID: input.ID, Status: domain.OJ_WA}
	if input.SQL == nil || !strings.EqualFold(input.SQL.Dialect, "mysql") {
		return result, errors.New("a MySQL evaluation contract is required")
	}
	if len(input.SQL.TestCases) == 0 {
		return result, errors.New("at least one SQL test case is required")
	}
	result.TotalCases = len(input.SQL.TestCases)
	failureStatus := domain.OJ_WA
	maxRows := normalizeMaxRows(input.SQL.MaxRows)
	timeLimit := time.Duration(input.RunLimits.Time) * time.Second
	if timeLimit <= 0 || timeLimit > 30*time.Second {
		timeLimit = 5 * time.Second
	}

	for index, testCase := range input.SQL.TestCases {
		started := time.Now()
		caseResult := domain.TestCaseResult{Index: index, Status: domain.OJ_WA}
		expectedJSON, _ := json.Marshal(testCase.Expected)
		caseResult.Expected = string(expectedJSON)
		actual, status, err := r.evaluateCase(ctx, input.SourceCode, testCase, maxRows, timeLimit)
		caseResult.ExecutionTime = time.Since(started).Seconds()
		caseResult.Status = status
		if actual != nil {
			actualJSON, _ := json.Marshal(actual)
			caseResult.Received = string(actualJSON)
		}
		if err != nil {
			caseResult.ErrorMessage = err.Error()
			if status != domain.OJ_WA && failureStatus == domain.OJ_WA {
				failureStatus = status
			}
		}
		if err == nil && EqualResultSets(testCase.Expected, *actual, input.SQL.OrderMatters) {
			caseResult.Passed = true
			caseResult.Status = domain.OJ_AC
			result.TotalPassed++
		}
		result.Results = append(result.Results, caseResult)
	}
	if result.TotalPassed == result.TotalCases {
		result.Status = domain.OJ_AC
	} else {
		result.Status = failureStatus
	}
	return result, nil
}

func (r *Runner) evaluateCase(parent context.Context, query string, testCase domain.SQLTestCase, maxRows int, timeout time.Duration) (*domain.SQLResultSet, int, error) {
	databaseName, username, password, err := randomCredentials()
	if err != nil {
		return nil, domain.OJ_RE, err
	}
	admin, err := sql.Open("mysql", r.adminConfig.FormatDSN())
	if err != nil {
		return nil, domain.OJ_RE, err
	}
	defer admin.Close()
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		return nil, domain.OJ_RE, fmt.Errorf("MySQL sandbox unavailable: %w", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+databaseName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		return nil, domain.OJ_RE, fmt.Errorf("create sandbox database: %w", err)
	}
	defer admin.Exec("DROP DATABASE IF EXISTS `" + databaseName + "`")
	if _, err := admin.ExecContext(ctx, "CREATE USER '"+username+"'@'%' IDENTIFIED BY '"+password+"'"); err != nil {
		return nil, domain.OJ_RE, fmt.Errorf("create sandbox user: %w", err)
	}
	defer admin.Exec("DROP USER IF EXISTS '" + username + "'@'%'")
	fixtureConfig := r.adminConfig
	fixtureConfig.DBName = databaseName
	fixtureConfig.MultiStatements = true
	fixtures, err := sql.Open("mysql", fixtureConfig.FormatDSN())
	if err != nil {
		return nil, domain.OJ_RE, err
	}
	defer fixtures.Close()
	if strings.TrimSpace(testCase.Schema) != "" {
		if _, err := fixtures.ExecContext(ctx, testCase.Schema); err != nil {
			return nil, domain.OJ_RE, fmt.Errorf("invalid trusted schema: %w", err)
		}
	}
	if strings.TrimSpace(testCase.Seed) != "" {
		if _, err := fixtures.ExecContext(ctx, testCase.Seed); err != nil {
			return nil, domain.OJ_RE, fmt.Errorf("invalid trusted seed: %w", err)
		}
	}
	if _, err := admin.ExecContext(ctx, "GRANT SELECT ON `"+databaseName+"`.* TO '"+username+"'@'%'"); err != nil {
		return nil, domain.OJ_RE, fmt.Errorf("grant sandbox permissions: %w", err)
	}

	contestantConfig := r.adminConfig
	contestantConfig.User, contestantConfig.Passwd, contestantConfig.DBName = username, password, databaseName
	contestantConfig.MultiStatements = false
	contestant, err := sql.Open("mysql", contestantConfig.FormatDSN())
	if err != nil {
		return nil, domain.OJ_RE, err
	}
	defer contestant.Close()
	connection, err := contestant.Conn(ctx)
	if err != nil {
		return nil, domain.OJ_RE, fmt.Errorf("open contestant connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "SET SESSION TRANSACTION READ ONLY"); err != nil {
		return nil, domain.OJ_RE, fmt.Errorf("enable read-only session: %w", err)
	}
	rows, err := connection.QueryContext(ctx, query)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, domain.OJ_TL, errors.New("SQL query exceeded the time limit")
		}
		return nil, domain.OJ_RE, fmt.Errorf("SQL execution failed: %w", err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, domain.OJ_RE, err
	}
	actual := &domain.SQLResultSet{Columns: columns}
	for rows.Next() {
		if len(actual.Rows) >= maxRows {
			return actual, domain.OJ_OL, fmt.Errorf("SQL result exceeds %d rows", maxRows)
		}
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return actual, domain.OJ_RE, err
		}
		row := make([]*string, len(values))
		for i, value := range values {
			if value == nil {
				continue
			}
			text := normalizeValue(value)
			row[i] = &text
		}
		actual.Rows = append(actual.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return actual, domain.OJ_RE, err
	}
	return actual, domain.OJ_WA, nil
}

func EqualResultSets(expected, actual domain.SQLResultSet, orderMatters bool) bool {
	if len(expected.Columns) != len(actual.Columns) || len(expected.Rows) != len(actual.Rows) {
		return false
	}
	for i := range expected.Columns {
		if !strings.EqualFold(strings.TrimSpace(expected.Columns[i]), strings.TrimSpace(actual.Columns[i])) {
			return false
		}
	}
	expectedRows, actualRows := canonicalRows(expected.Rows), canonicalRows(actual.Rows)
	if !orderMatters {
		sort.Strings(expectedRows)
		sort.Strings(actualRows)
	}
	for i := range expectedRows {
		if expectedRows[i] != actualRows[i] {
			return false
		}
	}
	return true
}

func canonicalRows(rows [][]*string) []string {
	result := make([]string, len(rows))
	for i, row := range rows {
		encoded, _ := json.Marshal(row)
		result[i] = string(encoded)
	}
	return result
}

func normalizeValue(value any) string {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(typed)
	}
}

func normalizeMaxRows(value int) int {
	if value <= 0 {
		return defaultMaxRows
	}
	if value > maxAllowedRows {
		return maxAllowedRows
	}
	return value
}

func randomCredentials() (string, string, string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", "", err
	}
	token := hex.EncodeToString(bytes)
	return "sql_" + token, "u_" + token, "P_" + token + "_9x", nil
}
