package httpapi

import (
	"strings"
	"testing"
	"time"
)

func TestCronPreviewUsesCurrentTimeAndSelectedTimezone(t *testing.T) {
	c := setupClient(t)
	if code, _ := c.call("GET", "/api/schedules/preview?cron=*+*+*+*+*", nil, false); code != 401 {
		t.Fatal("preview must require login", code)
	}
	_, v := c.call("GET", "/api/setup/status", nil, false)
	c.csrf = v["data"].(map[string]any)["csrf"].(string)
	creds := map[string]string{"username": "admin", "password": "test-password"}
	c.call("POST", "/api/setup", creds, true)
	_, v = c.call("POST", "/api/auth/login", creds, false)
	c.csrf = v["data"].(map[string]any)["csrf"].(string)
	for _, zone := range []string{"", "UTC", "America/New_York"} {
		before := time.Now()
		code, v := c.call("GET", "/api/schedules/preview?cron=*+*+*+*+*&timezone="+zone, nil, false)
		if code != 200 {
			t.Fatalf("preview %d %v", code, v)
		}
		data := v["data"].(map[string]any)
		now, err := time.Parse(time.RFC3339Nano, data["now"].(string))
		if err != nil || now.Before(before) || now.After(time.Now()) {
			t.Fatal("preview not based on current server time", data, err)
		}
		if zone == "" {
			zone = "Asia/Shanghai"
		}
		if data["timezone"] != zone {
			t.Fatal("unexpected preview timezone", data)
		}
		loc, _ := time.LoadLocation(zone)
		times := data["times"].([]any)
		if len(times) != 3 {
			t.Fatal("expected three future times", times)
		}
		for i, value := range times {
			got, err := time.Parse(time.RFC3339, value.(string))
			want := now.Truncate(time.Minute).Add(time.Duration(i+1) * time.Minute)
			_, gotOffset := got.Zone()
			_, wantOffset := want.In(loc).Zone()
			if err != nil || !got.Equal(want) || gotOffset != wantOffset {
				t.Fatalf("time %d got=%v want=%v err=%v", i, got, want.In(loc), err)
			}
		}
	}
	for _, query := range []string{"cron=invalid", "cron=0+0+8+*+*+*", "cron=0+0+30+2+*", "cron=*+*+*+*+*&timezone=Mars/City"} {
		code, v := c.call("GET", "/api/schedules/preview?"+query, nil, false)
		if code != 400 || !strings.Contains(v["error"].(map[string]any)["message"].(string), "Cron") && !strings.Contains(v["error"].(map[string]any)["message"].(string), "时区") {
			t.Fatalf("invalid preview accepted: %d %v", code, v)
		}
	}
	tasks, err := c.db.ListTasks(t.Context())
	if err != nil || len(tasks) != 0 {
		t.Fatal("preview must not create tasks", tasks, err)
	}
}
