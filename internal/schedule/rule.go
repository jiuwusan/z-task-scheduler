package schedule

import (
	"errors"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"github.com/robfig/cron/v3"
	"strings"
	"time"
	_ "time/tzdata"
)

func zone(r domain.Schedule) (*time.Location, error) {
	if r.Timezone == "" {
		r.Timezone = "Asia/Shanghai"
	}
	return time.LoadLocation(r.Timezone)
}
func Validate(r domain.Schedule, now time.Time) error {
	if _, err := zone(r); err != nil {
		return errors.New("时区无效")
	}
	switch r.Kind {
	case "cron":
		if len(strings.Fields(r.Cron)) != 5 {
			return errors.New("Cron 必须为五字段：分 时 日 月 周")
		}
		if _, err := cron.ParseStandard(r.Cron); err != nil {
			return errors.New("Cron 表达式无效")
		}
	case "interval":
		if r.IntervalSeconds < 60 || r.IntervalSeconds > 31536000 {
			return errors.New("固定间隔必须为 60 秒至一年")
		}
	case "once":
		if r.At == nil || !r.At.After(now) {
			return errors.New("单次执行时间必须晚于当前时间")
		}
	default:
		return errors.New("未知调度类型")
	}
	return nil
}
func Next(r domain.Schedule, after time.Time) (*time.Time, error) {
	if err := Validate(r, after); err != nil {
		return nil, err
	}
	var next time.Time
	switch r.Kind {
	case "cron":
		loc, _ := zone(r)
		s, _ := cron.ParseStandard(r.Cron)
		next = s.Next(after.In(loc))
		if next.IsZero() {
			return nil, errors.New("Cron 没有可用的未来执行时间")
		}
	case "interval":
		next = after.Add(time.Duration(r.IntervalSeconds) * time.Second)
	case "once":
		next = *r.At
	}
	return &next, nil
}
func Advance(r domain.Schedule, previous, now time.Time) (*time.Time, error) {
	if r.Kind == "once" {
		return nil, nil
	}
	if r.Kind != "interval" {
		return Next(r, now)
	}
	if err := Validate(r, now); err != nil {
		return nil, err
	}
	step := time.Duration(r.IntervalSeconds) * time.Second
	next := previous.Add(step)
	if !next.After(now) {
		next = previous.Add((now.Sub(previous)/step + 1) * step)
	}
	return &next, nil
}
