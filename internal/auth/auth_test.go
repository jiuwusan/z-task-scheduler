package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordAndSession(t *testing.T) {
	if _, err := Hash("short"); err == nil {
		t.Fatal("short password")
	}
	if _, err := Hash(strings.Repeat("a", 73)); err == nil {
		t.Fatal("bcrypt truncation")
	}
	h, err := Hash("long-password")
	if err != nil || !Verify(h, "long-password") || Verify(h, "incorrect") {
		t.Fatal("hash verification", err)
	}
	s := NewSessions()
	now := time.Now()
	v, err := s.Create("admin", now)
	if err != nil || v.Token == v.CSRF || v.Token == "" {
		t.Fatal(v, err)
	}
	if _, ok := s.Get(v.Token, now.Add(25*time.Hour)); ok {
		t.Fatal("expired session")
	}
	v, _ = s.Create("admin", now)
	s.Delete(v.Token)
	if _, ok := s.Get(v.Token, now); ok {
		t.Fatal("deleted session")
	}
}
