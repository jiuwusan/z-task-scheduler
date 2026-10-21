package httpapi

import (
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"github.com/jiuwusan/z-task-scheduler/internal/schedule"
	"net/http"
	"time"
)

func (s *server) previewSchedule(w http.ResponseWriter, r *http.Request) {
	rule := domain.Schedule{Kind: "cron", Cron: r.URL.Query().Get("cron"), Timezone: r.URL.Query().Get("timezone")}
	if rule.Timezone == "" {
		rule.Timezone = "Asia/Shanghai"
	}
	now := time.Now().UTC()
	after := now
	times := make([]time.Time, 0, 3)
	for range 3 {
		next, err := schedule.Next(rule, after)
		if err != nil {
			fail(w, 400, "INVALID_SCHEDULE", err.Error())
			return
		}
		times = append(times, *next)
		after = *next
	}
	respond(w, 200, map[string]any{"now": now, "timezone": rule.Timezone, "times": times})
}
