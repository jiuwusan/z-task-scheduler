package schedule_test

import (
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"github.com/jiuwusan/z-task-scheduler/internal/schedule"
	"testing"
	"time"
)

func TestFiveFieldCron(t *testing.T) {
	now := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	rule := domain.Schedule{Kind: "cron", Cron: "0 8 * * *", Timezone: "Asia/Shanghai"}
	next, err := schedule.Next(rule, now)
	if err != nil || next == nil || next.Format(time.RFC3339) != "2026-10-21T08:00:00+08:00" {
		t.Fatalf("next=%v err=%v", next, err)
	}
	rule.Cron = "0 0 8 * * *"
	if schedule.Validate(rule, now) == nil {
		t.Fatal("accepted seconds field")
	}
}
func TestIntervalAndTimeoutBounds(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	rule := domain.Schedule{Kind: "interval", IntervalSeconds: 59, Timezone: "Asia/Shanghai"}
	if schedule.Validate(rule, now) == nil {
		t.Fatal("accepted 59 seconds")
	}
	rule.IntervalSeconds = 60
	if err := schedule.Validate(rule, now); err != nil {
		t.Fatal(err)
	}
	next, err := schedule.Advance(rule, now, now.Add(185*time.Second))
	if err != nil || !next.Equal(now.Add(240*time.Second)) {
		t.Fatalf("%v %v", next, err)
	}
	for _, sec := range []int{-1, 3601} {
		task := domain.Task{Name: "a", Type: "http", URL: "http://localhost", TimeoutSeconds: sec}
		if domain.ValidateTask(&task) == nil {
			t.Fatalf("accepted %d", sec)
		}
	}
	task := domain.Task{Name: "a", Type: "http", URL: "http://localhost"}
	if err := domain.ValidateTask(&task); err != nil || task.TimeoutSeconds != 60 {
		t.Fatalf("%+v %v", task, err)
	}
}
func TestSingleRunRequiresFuture(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Second)
	rule := domain.Schedule{Kind: "once", At: &past}
	if schedule.Validate(rule, now) == nil {
		t.Fatal("accepted past")
	}
	future := now.Add(time.Minute)
	rule.At = &future
	if err := schedule.Validate(rule, now); err != nil {
		t.Fatal(err)
	}
}
func TestInvalidTimezoneAndDST(t *testing.T) {
	now := time.Date(2026, 3, 8, 6, 0, 0, 0, time.UTC)
	rule := domain.Schedule{Kind: "cron", Cron: "30 2 * * *", Timezone: "America/New_York"}
	next, err := schedule.Next(rule, now)
	if err != nil || next == nil || !next.After(now) {
		t.Fatalf("%v %v", next, err)
	}
	rule.Timezone = "Mars/City"
	if schedule.Validate(rule, now) == nil {
		t.Fatal("invalid zone accepted")
	}
}
