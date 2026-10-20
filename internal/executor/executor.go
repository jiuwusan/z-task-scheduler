package executor

import (
	"context"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
)

type Executor interface {
	Execute(context.Context, domain.Task) domain.RunResult
}
