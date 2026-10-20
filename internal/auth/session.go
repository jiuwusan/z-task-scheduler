package auth

import (
	"crypto/rand"
	"sync"
	"time"
)

type Session struct {
	Token     string    `json:"-"`
	CSRF      string    `json:"csrf"`
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type Sessions struct {
	mu    sync.Mutex
	items map[string]Session
}

func NewSessions() *Sessions { return &Sessions{items: map[string]Session{}} }
func (s *Sessions) Create(username string, now time.Time) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.items {
		if !v.ExpiresAt.After(now) {
			delete(s.items, k)
		}
	}
	v := Session{Token: rand.Text(), CSRF: rand.Text(), Username: username, ExpiresAt: now.Add(24 * time.Hour)}
	s.items[v.Token] = v
	return v, nil
}
func (s *Sessions) Get(token string, now time.Time) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[token]
	if !ok || !v.ExpiresAt.After(now) {
		delete(s.items, token)
		return Session{}, false
	}
	return v, true
}
func (s *Sessions) Delete(token string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.items, token) }
