package services

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/samuelloza/isolate-wrapper/src/application/abstractions"
	"github.com/samuelloza/isolate-wrapper/src/domain"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/comparator"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/compiler"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/fileSystem"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/isolate"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/mysqlrunner"
)

type RequestProcessor struct {
	Input            domain.EvaluationInput
	BoxPool          *BoxPool
	TestCaseProvider abstractions.TestCaseProvider
}

func (rp *RequestProcessor) ProcessRequest() (domain.EvaluationResult, error) {
	if strings.EqualFold(rp.Input.Language, "mysql") || strings.EqualFold(rp.Input.Language, "sql") {
		runner, err := mysqlrunner.New(os.Getenv("MYSQL_SANDBOX_ADMIN_DSN"))
		if err != nil {
			return domain.EvaluationResult{SubmitID: rp.Input.ID, Status: domain.OJ_RE}, err
		}
		return runner.Evaluate(context.Background(), rp.Input)
	}

	cases, err := rp.TestCaseProvider.GetTestCases(fmt.Sprintf("%d", rp.Input.ProblemID))
	if err != nil {
		return domain.EvaluationResult{SubmitID: rp.Input.ID, Status: domain.OJ_RE}, fmt.Errorf("get test cases: %w", err)
	}
	rp.Input.TestCases = cases

	SandboxManagerService := NewSandboxManagerService()
	boxId, err := SandboxManagerService.GetAvailableSandboxID(rp.Input.BoxID, rp.BoxPool)
	if err != nil {
		return domain.EvaluationResult{SubmitID: rp.Input.ID, Status: domain.OJ_RE}, fmt.Errorf("get sandbox: %w", err)
	}
	rp.Input.BoxID = boxId

	sandboxImpl := &isolate.IsolateSandbox{}
	compilerImpl, err := compiler.GetCompiler(rp.Input.Language, rp.Input.BoxID)
	if err != nil {
		return domain.EvaluationResult{SubmitID: rp.Input.ID, Status: domain.OJ_CE}, fmt.Errorf("select compiler: %w", err)
	}
	fileSystemImpl := &fileSystem.FileSystem{}
	comparatorImpl := &comparator.Comparator{}

	evaluator := NewEvaluatorService(sandboxImpl, compilerImpl, fileSystemImpl, comparatorImpl)

	result, err := evaluator.Evaluate(rp.Input)
	if err != nil {
		return result, fmt.Errorf("evaluate submission: %w", err)
	}

	//spew.Dump(result)
	return result, nil
}
