package abstractions

import (
	"context"

	"github.com/samuelloza/isolate-wrapper/src/domain"
)

type SQLRunner interface {
	Evaluate(context.Context, domain.EvaluationInput) (domain.EvaluationResult, error)
}
