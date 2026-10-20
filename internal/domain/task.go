package domain

import (
	"errors"
	"strings"
	"time"
)

type Schedule struct {
	Kind            string     `json:"kind"`
	Cron            string     `json:"cron,omitempty"`
	Timezone        string     `json:"timezone"`
	IntervalSeconds int        `json:"intervalSeconds,omitempty"`
	At              *time.Time `json:"at,omitempty"`
}
type Task struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	CurlCommand    string     `json:"curlCommand,omitempty"`
	ContainerID    string     `json:"containerId,omitempty"`
	ContainerName  string     `json:"containerName,omitempty"`
	DockerAction   string     `json:"dockerAction,omitempty"`
	Enabled        bool       `json:"enabled"`
	TimeoutSeconds int        `json:"timeoutSeconds"`
	Schedule       Schedule   `json:"schedule"`
	NextRunAt      *time.Time `json:"nextRunAt,omitempty"`
}

func ValidateTask(t *Task) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || len(t.Name) > 200 {
		return errors.New("任务名称不能为空，且不能超过 200 字节")
	}
	if t.TimeoutSeconds == 0 {
		t.TimeoutSeconds = 60
	}
	if t.TimeoutSeconds < 1 || t.TimeoutSeconds > 3600 {
		return errors.New("超时必须为 1 至 3600 秒")
	}
	if t.Schedule.Timezone == "" {
		t.Schedule.Timezone = "Asia/Shanghai"
	}
	switch t.Type {
	case "curl":
		if strings.TrimSpace(t.CurlCommand) == "" || len(t.CurlCommand) > 65536 {
			return errors.New("请输入 curl 命令，最长 64 KiB")
		}
	case "docker":
		if t.ContainerID == "" {
			return errors.New("请选择容器")
		}
		if t.DockerAction != "start" && t.DockerAction != "stop" && t.DockerAction != "restart" {
			return errors.New("容器操作仅支持 start、stop、restart")
		}
	default:
		return errors.New("未知任务类型")
	}
	return nil
}
