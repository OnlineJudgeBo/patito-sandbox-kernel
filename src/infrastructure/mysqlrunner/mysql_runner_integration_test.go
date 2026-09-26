package mysqlrunner

import (
	"context"
	"os"
	"testing"

	"github.com/samuelloza/isolate-wrapper/src/domain"
)

func TestRunnerEvaluatesReadOnlyMySQLQuery(t *testing.T) {
	dsn := os.Getenv("MYSQL_SANDBOX_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_SANDBOX_TEST_DSN is not configured")
	}
	runner, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	input := domain.EvaluationInput{
		ID: "sql-test", Language: "mysql", SourceCode: "SELECT name FROM vectors WHERE dimension >= 3 ORDER BY name",
		RunLimits: domain.RunLimits{Time: 5},
		SQL: &domain.SQLEvaluationInput{Dialect: "mysql", OrderMatters: true, MaxRows: 100, TestCases: []domain.SQLTestCase{{
			Schema:   "CREATE TABLE vectors (id INT PRIMARY KEY, name VARCHAR(50), dimension INT)",
			Seed:     "INSERT INTO vectors VALUES (1, 'vector-a', 2), (2, 'vector-b', 3), (3, 'vector-c', 4)",
			Expected: domain.SQLResultSet{Columns: []string{"name"}, Rows: [][]*string{{cell("vector-b")}, {cell("vector-c")}}},
		}}},
	}
	result, err := runner.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.OJ_AC || result.TotalPassed != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunnerRejectsMutation(t *testing.T) {
	dsn := os.Getenv("MYSQL_SANDBOX_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_SANDBOX_TEST_DSN is not configured")
	}
	runner, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	input := domain.EvaluationInput{
		ID: "sql-write-test", Language: "mysql", SourceCode: "DELETE FROM vectors",
		RunLimits: domain.RunLimits{Time: 5},
		SQL:       &domain.SQLEvaluationInput{Dialect: "mysql", TestCases: []domain.SQLTestCase{{Schema: "CREATE TABLE vectors (id INT PRIMARY KEY)"}}},
	}
	result, err := runner.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.OJ_RE || len(result.Results) != 1 || result.Results[0].ErrorMessage == "" {
		t.Fatalf("a mutation must be rejected as a runtime error: %+v", result)
	}
}
