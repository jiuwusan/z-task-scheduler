package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHealthcheckCommand(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	for _, status := range []int{200, 503} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		os.Args = []string{"scheduler", "healthcheck", "-url", s.URL}
		err := run()
		s.Close()
		if (status == 200) != (err == nil) {
			t.Fatalf("status=%d error=%v", status, err)
		}
	}
}
