package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
)

var ErrNotFound = sql.ErrNoRows
var ErrInitialized = errors.New("管理员已初始化")

type Store struct{ db *sql.DB }

func ID() string { return rand.Text() }
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("创建数据目录: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("打开数据库: %w", err)
	}
	f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化数据库: %w", err)
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func affected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}
func (s *Store) Admin(ctx context.Context) (string, string, error) {
	var u, h string
	err := s.db.QueryRowContext(ctx, "SELECT username,password_hash FROM admin WHERE id=1").Scan(&u, &h)
	return u, h, err
}
func (s *Store) InitializeAdmin(ctx context.Context, u, h string) error {
	r, err := s.db.ExecContext(ctx, "INSERT OR IGNORE INTO admin(id,username,password_hash) VALUES(1,?,?)", u, h)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n == 0 {
		return ErrInitialized
	}
	return err
}
