package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Container struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
	Self  bool   `json:"self"`
}
type Docker struct {
	client                       *http.Client
	baseURL, selfName, selfLabel string
	hostname                     string
}

func NewDocker(socket, selfName, selfLabel string) *Docker {
	hostname, _ := os.Hostname()
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	return &Docker{baseURL: "http://docker", selfName: selfName, selfLabel: selfLabel, hostname: hostname, client: &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}}
}
func (d *Docker) request(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, d.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == 304 {
			return nil
		}
		return fmt.Errorf("Docker API 返回 HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
	}
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
	return err
}
func (d *Docker) version(ctx context.Context) (string, error) {
	var v struct {
		APIVersion string `json:"ApiVersion"`
		Min        string `json:"MinAPIVersion"`
	}
	if err := d.request(ctx, "GET", "/version", &v); err != nil {
		return "", fmt.Errorf("无法连接 Docker，请检查 socket 挂载和组权限: %w", err)
	}
	minor := func(s string) (int, error) {
		if !strings.HasPrefix(s, "1.") {
			return 0, errors.New("不支持的 Docker API 版本")
		}
		return strconv.Atoi(strings.TrimPrefix(s, "1."))
	}
	n, err := minor(v.APIVersion)
	if err != nil {
		return "", err
	}
	if n < 24 {
		return "", errors.New("Docker API 版本过旧")
	}
	if v.Min != "" {
		minimum, err := minor(v.Min)
		if err != nil || minimum > 45 {
			return "", errors.New("Docker API 最低版本高于客户端支持版本")
		}
	}
	return "v1." + strconv.Itoa(min(n, 45)), nil
}

type inspection struct {
	ID     string `json:"Id"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
}

func (d *Docker) inspect(ctx context.Context, v, id string) (inspection, error) {
	var out inspection
	err := d.request(ctx, "GET", "/"+v+"/containers/"+url.PathEscape(id)+"/json", &out)
	return out, err
}
func (d *Docker) identity(ctx context.Context, v string) (string, error) {
	s, err := d.inspect(ctx, v, d.selfName)
	if err != nil || s.ID == "" || s.Config.Labels[d.selfLabel] != "true" {
		return "", errors.New("无法识别调度器自身，请检查容器名称和识别标签")
	}
	if d.hostname == "" || (s.ID != d.hostname && (len(d.hostname) < 12 || !strings.HasPrefix(s.ID, d.hostname))) {
		return "", errors.New("调度器名称对应的容器与当前实例身份不匹配，请保留默认容器 hostname")
	}
	return s.ID, nil
}
func (d *Docker) Status(ctx context.Context) error {
	v, err := d.version(ctx)
	if err != nil {
		return err
	}
	_, err = d.identity(ctx, v)
	return err
}
func (d *Docker) List(ctx context.Context) ([]Container, error) {
	v, err := d.version(ctx)
	if err != nil {
		return nil, err
	}
	self, err := d.identity(ctx, v)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID    string   `json:"Id"`
		Names []string `json:"Names"`
		State string   `json:"State"`
	}
	if err = d.request(ctx, "GET", "/"+v+"/containers/json?all=1", &rows); err != nil {
		return nil, err
	}
	out := []Container{}
	for _, r := range rows {
		name := r.ID
		if len(r.Names) > 0 {
			name = strings.TrimPrefix(r.Names[0], "/")
		}
		out = append(out, Container{ID: r.ID, Name: name, State: r.State, Self: r.ID == self})
	}
	return out, nil
}
func (d *Docker) ValidateTarget(ctx context.Context, id string) error {
	v, err := d.version(ctx)
	if err != nil {
		return err
	}
	self, err := d.identity(ctx, v)
	if err != nil {
		return err
	}
	if id == self {
		return errors.New("禁止操作调度器自身")
	}
	target, err := d.inspect(ctx, v, id)
	if err != nil {
		return fmt.Errorf("目标容器不存在或无法访问，请重新选择: %w", err)
	}
	if target.ID != id {
		return errors.New("请使用完整容器 ID，容器可能已重建")
	}
	return nil
}
func (d *Docker) Execute(ctx context.Context, t domain.Task) domain.RunResult {
	failed := func(err error) domain.RunResult { return domain.RunResult{State: "failed", Error: err.Error()} }
	if t.DockerAction != "start" && t.DockerAction != "stop" && t.DockerAction != "restart" {
		return failed(errors.New("未知容器操作"))
	}
	v, err := d.version(ctx)
	if err != nil {
		return failed(err)
	}
	self, err := d.identity(ctx, v)
	if err != nil {
		return failed(err)
	}
	if t.ContainerID == self {
		return failed(errors.New("禁止操作调度器自身"))
	}
	target, err := d.inspect(ctx, v, t.ContainerID)
	if err != nil || target.ID != t.ContainerID {
		return failed(errors.New("目标容器已删除或发生变化，请重新选择"))
	}
	if (t.DockerAction == "start" && target.State.Running) || (t.DockerAction == "stop" && !target.State.Running) {
		return domain.RunResult{State: "success", Output: "容器已处于目标状态，无需操作"}
	}
	path := "/" + v + "/containers/" + url.PathEscape(t.ContainerID) + "/" + t.DockerAction
	if t.DockerAction != "start" {
		grace := 10
		if deadline, ok := ctx.Deadline(); ok {
			grace = max(0, min(10, int(time.Until(deadline).Seconds())-1))
		}
		path += "?t=" + strconv.Itoa(grace)
	}
	if err = d.request(ctx, "POST", path, nil); err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) || ctx.Err() != nil {
			return domain.RunResult{State: "unknown", Error: "Docker 请求结果未知，请检查容器实际状态"}
		}
		return failed(err)
	}
	return domain.RunResult{State: "success", Output: "已完成容器 " + t.DockerAction + " 操作"}
}
