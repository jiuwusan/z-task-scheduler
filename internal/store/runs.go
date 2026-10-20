package store

import (
	"context"
	"encoding/json"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"github.com/jiuwusan/z-task-scheduler/internal/schedule"
	"time"
)

func (s *Store) CreateRun(ctx context.Context, r domain.Run) (domain.Run, error) {
	r.ID = ID()
	b, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO runs(id,task_id,started,state,payload) VALUES(?,?,?,?,?)", r.ID, r.TaskID, r.StartedAt.UnixMilli(), r.State, string(b))
	return r, err
}
func (s *Store) UpdateRun(ctx context.Context, r domain.Run) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return affected(s.db.ExecContext(ctx, "UPDATE runs SET state=?,payload=? WHERE id=?", r.State, string(b), r.ID))
}
func (s *Store) GetRun(ctx context.Context, id string) (domain.Run, error) {
	var r domain.Run
	var b string
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM runs WHERE id=?", id).Scan(&b)
	if err == nil {
		err = json.Unmarshal([]byte(b), &r)
	}
	return r, err
}
func (s *Store) ListRuns(ctx context.Context, taskID string, page, limit int) ([]domain.Run, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	clause := ""
	args := []any{}
	if taskID != "" {
		clause = " WHERE task_id=?"
		args = append(args, taskID)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM runs"+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := s.db.QueryContext(ctx, "SELECT payload FROM runs"+clause+" ORDER BY started DESC,id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.Run{}
	for rows.Next() {
		var b string
		var r domain.Run
		if err = rows.Scan(&b); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal([]byte(b), &r); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}
func (s *Store) Prune(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM runs WHERE started<? AND state!='running'", before.UnixMilli())
	return err
}
func (s *Store) Recover(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT payload FROM runs WHERE state='running'")
	if err != nil {
		return err
	}
	runs := []domain.Run{}
	for rows.Next() {
		var b string
		var r domain.Run
		if err = rows.Scan(&b); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(b), &r); err != nil {
			rows.Close()
			return err
		}
		runs = append(runs, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range runs {
		r.State = "interrupted"
		r.Result = domain.RunResult{State: "interrupted", Error: "服务在执行完成前停止，请检查实际操作结果"}
		r.FinishedAt = &now
		b, _ := json.Marshal(r)
		if _, err = tx.ExecContext(ctx, "UPDATE runs SET state=?,payload=? WHERE id=?", r.State, string(b), r.ID); err != nil {
			return err
		}
	}
	rows, err = tx.QueryContext(ctx, "SELECT payload FROM tasks")
	if err != nil {
		return err
	}
	tasks := []domain.Task{}
	for rows.Next() {
		var b string
		var t domain.Task
		if err = rows.Scan(&b); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(b), &t); err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if !t.Enabled {
			continue
		}
		if t.Schedule.Kind == "once" && (t.Schedule.At == nil || !t.Schedule.At.After(now)) {
			t.Enabled = false
			t.NextRunAt = nil
		} else if t.NextRunAt == nil || !t.NextRunAt.After(now) {
			t.NextRunAt, err = schedule.Next(t.Schedule, now)
			if err != nil {
				return err
			}
		}
		b, _ := json.Marshal(t)
		if _, err = tx.ExecContext(ctx, "UPDATE tasks SET payload=? WHERE id=?", string(b), t.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
