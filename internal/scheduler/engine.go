package scheduler

import (
	"context"
	"errors"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"github.com/jiuwusan/z-task-scheduler/internal/executor"
	"github.com/jiuwusan/z-task-scheduler/internal/schedule"
	"github.com/jiuwusan/z-task-scheduler/internal/store"
	"log/slog"
	"sync"
	"time"
)

var ErrBusy = errors.New("任务、目标容器正在执行，或已达到并发上限")
var ErrClosed = errors.New("服务正在关闭")

type Engine struct {
	db                 *store.Store
	curl, docker       executor.Executor
	mu                 sync.Mutex
	active, containers map[string]bool
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	closed, started    bool
}

func New(db *store.Store, curl, docker executor.Executor) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{db: db, curl: curl, docker: docker, active: map[string]bool{}, containers: map[string]bool{}, ctx: ctx, cancel: cancel}
}
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started {
		return errors.New("调度器已启动")
	}
	if e.closed {
		return ErrClosed
	}
	if err := e.db.Recover(ctx, time.Now()); err != nil {
		return err
	}
	e.started = true
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		prune := time.NewTicker(24 * time.Hour)
		defer prune.Stop()
		if err := e.db.Prune(e.ctx, time.Now().Add(-30*24*time.Hour)); err != nil {
			slog.Error("清理历史失败", "error", err)
		}
		for {
			select {
			case <-ctx.Done():
				e.cancel()
				return
			case <-e.ctx.Done():
				return
			case now := <-tick.C:
				if err := e.Tick(e.ctx, now); err != nil && !errors.Is(err, context.Canceled) {
					slog.Error("调度失败", "error", err)
				}
			case now := <-prune.C:
				if err := e.db.Prune(e.ctx, now.Add(-30*24*time.Hour)); err != nil {
					slog.Error("清理历史失败", "error", err)
				}
			}
		}
	}()
	return nil
}
func (e *Engine) SaveTask(ctx context.Context, t domain.Task, now time.Time) (domain.Task, error) {
	if err := domain.ValidateTask(&t); err != nil {
		return t, err
	}
	checkNow := now
	if !t.Enabled && t.Schedule.Kind == "once" && t.Schedule.At != nil {
		checkNow = t.Schedule.At.Add(-time.Nanosecond)
	}
	if err := schedule.Validate(t.Schedule, checkNow); err != nil {
		return t, err
	}
	if t.Type == "curl" {
		if _, err := executor.ParseCurl(t.CurlCommand); err != nil {
			return t, err
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return t, ErrClosed
	}
	t.NextRunAt = nil
	if t.Enabled {
		next, err := schedule.Next(t.Schedule, now)
		if err != nil {
			return t, err
		}
		t.NextRunAt = next
	}
	if t.ID == "" {
		return e.db.CreateTask(ctx, t)
	}
	if _, err := e.db.GetTask(ctx, t.ID); err != nil {
		return t, err
	}
	return t, e.db.UpdateTask(ctx, t)
}
func (e *Engine) DeleteTask(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	if e.active[id] {
		return ErrBusy
	}
	return e.db.DeleteTask(ctx, id)
}
func (e *Engine) RunNow(ctx context.Context, id string) (domain.Run, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return domain.Run{}, ErrClosed
	}
	t, err := e.db.GetTask(ctx, id)
	if err != nil {
		return domain.Run{}, err
	}
	return e.dispatch(ctx, t, "manual", time.Now())
}
func (e *Engine) Tick(ctx context.Context, now time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	tasks, err := e.db.ListTasks(ctx)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.Enabled && t.NextRunAt != nil && !t.NextRunAt.After(now) {
			if _, err = e.dispatch(ctx, t, "schedule", now); err != nil && !errors.Is(err, ErrBusy) {
				return err
			}
		}
	}
	return nil
}
func (e *Engine) dispatch(ctx context.Context, t domain.Task, trigger string, now time.Time) (domain.Run, error) {
	busy := e.active[t.ID] || len(e.active) >= 4 || (t.Type == "docker" && e.containers[t.ContainerID])
	if busy && trigger == "manual" {
		return domain.Run{}, ErrBusy
	}
	r := domain.Run{TaskID: t.ID, TaskName: t.Name, Trigger: trigger, State: "running", StartedAt: now, Snapshot: t, Result: domain.RunResult{State: "running"}}
	if trigger == "schedule" {
		next, err := schedule.Advance(t.Schedule, *t.NextRunAt, now)
		if err != nil {
			return r, err
		}
		t.NextRunAt = next
		if t.Schedule.Kind == "once" {
			t.Enabled = false
		}
		if err = e.db.UpdateTask(ctx, t); err != nil {
			return r, err
		}
	}
	if busy {
		r.State = "skipped"
		r.Result = domain.RunResult{State: "skipped", Error: ErrBusy.Error()}
		r.FinishedAt = &now
	}
	r, err := e.db.CreateRun(ctx, r)
	if err != nil {
		return r, err
	}
	if busy {
		return r, ErrBusy
	}
	e.active[t.ID] = true
	if t.Type == "docker" {
		e.containers[t.ContainerID] = true
	}
	e.wg.Add(1)
	go e.execute(t, r)
	return r, nil
}
func (e *Engine) execute(t domain.Task, r domain.Run) {
	defer e.wg.Done()
	defer func() {
		e.mu.Lock()
		delete(e.active, t.ID)
		if t.Type == "docker" {
			delete(e.containers, t.ContainerID)
		}
		e.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(e.ctx, time.Duration(t.TimeoutSeconds)*time.Second)
	defer cancel()
	func() {
		defer func() {
			if recover() != nil {
				r.Result = domain.RunResult{State: "failed", Error: "执行器异常，请检查服务日志"}
			}
		}()
		runner := e.curl
		if t.Type == "docker" {
			runner = e.docker
		}
		r.Result = runner.Execute(ctx, t)
	}()
	if r.Result.State == "" {
		r.Result.State = "failed"
		r.Result.Error = "执行器未返回有效状态"
	}
	r.State = r.Result.State
	finished := time.Now()
	r.FinishedAt = &finished
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer saveCancel()
	if err := e.db.UpdateRun(saveCtx, r); err != nil {
		slog.Error("保存执行结果失败", "runId", r.ID, "error", err)
	}
}
func (e *Engine) Close(ctx context.Context) error {
	e.mu.Lock()
	e.closed = true
	e.cancel()
	e.mu.Unlock()
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
