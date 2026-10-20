package domain

import "time"

type RunResult struct {
	State      string `json:"state"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	ExitCode   int    `json:"exitCode"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Truncated  bool   `json:"truncated"`
}
type Run struct {
	ID         string     `json:"id"`
	TaskID     string     `json:"taskId"`
	TaskName   string     `json:"taskName"`
	Trigger    string     `json:"trigger"`
	State      string     `json:"state"`
	Snapshot   Task       `json:"-"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Result     RunResult  `json:"result"`
}
