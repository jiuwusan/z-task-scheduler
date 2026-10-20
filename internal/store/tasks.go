package store

import (
	"context"
	"encoding/json"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
)

func (s *Store) CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	t.ID = ID()
	b, err := json.Marshal(t)
	if err != nil {
		return t, err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO tasks(id,payload) VALUES(?,?)", t.ID, string(b))
	return t, err
}
func (s *Store) UpdateTask(ctx context.Context, t domain.Task) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return affected(s.db.ExecContext(ctx, "UPDATE tasks SET payload=? WHERE id=?", string(b), t.ID))
}
func (s *Store) GetTask(ctx context.Context, id string) (domain.Task, error) {
	var t domain.Task
	var b string
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM tasks WHERE id=?", id).Scan(&b)
	if err == nil {
		err = json.Unmarshal([]byte(b), &t)
	}
	return t, err
}
func (s *Store) ListTasks(ctx context.Context) ([]domain.Task, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT payload FROM tasks ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Task{}
	for rows.Next() {
		var b string
		var t domain.Task
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(b), &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) DeleteTask(ctx context.Context, id string) error {
	return affected(s.db.ExecContext(ctx, "DELETE FROM tasks WHERE id=?", id))
}
