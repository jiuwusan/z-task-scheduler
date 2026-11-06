package service

import (
	"context"
	"errors"
	"github.com/jiuwusan/z-task-scheduler/internal/auth"
	"github.com/jiuwusan/z-task-scheduler/internal/store"
	"strings"
)

type AccountStore interface {
	Admin(context.Context) (string, string, error)
	InitializeAdmin(context.Context, string, string) error
}
type Accounts struct{ Store AccountStore }
type AccountError struct {
	Status        int
	Code, Message string
}

func (e *AccountError) Error() string { return e.Message }
func (s *Accounts) Initialized(ctx context.Context) (bool, error) {
	_, _, err := s.Store.Admin(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
func (s *Accounts) Initialize(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 80 {
		return &AccountError{400, "INVALID_USERNAME", "请输入 1 至 80 字节的用户名"}
	}
	hash, err := auth.Hash(password)
	if err != nil {
		return &AccountError{400, "INVALID_PASSWORD", err.Error()}
	}
	if err = s.Store.InitializeAdmin(ctx, username, hash); errors.Is(err, store.ErrInitialized) {
		return &AccountError{409, "INITIALIZED", "管理员已创建"}
	}
	return err
}
func (s *Accounts) Authenticate(ctx context.Context, input, password string) (string, error) {
	username, hash, err := s.Store.Admin(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	valid := auth.Verify(hash, password)
	if err != nil || input != username || !valid {
		return "", &AccountError{401, "INVALID_CREDENTIALS", "用户名或密码错误"}
	}
	return username, nil
}
