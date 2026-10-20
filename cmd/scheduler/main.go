package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/jiuwusan/z-task-scheduler/internal/executor"
	"github.com/jiuwusan/z-task-scheduler/internal/httpapi"
	"github.com/jiuwusan/z-task-scheduler/internal/scheduler"
	"github.com/jiuwusan/z-task-scheduler/internal/store"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("服务退出", "error", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
		target := fs.String("url", "http://127.0.0.1:3000/health", "health endpoint")
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		c := http.Client{Timeout: 3 * time.Second}
		r, err := c.Get(*target)
		if err != nil {
			return err
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			return fmt.Errorf("health status %d", r.StatusCode)
		}
		return nil
	}
	listen := flag.String("listen", ":3000", "监听地址")
	data := flag.String("data-dir", "./data", "持久数据目录")
	socket := flag.String("docker-socket", "/var/run/docker.sock", "Docker Unix socket")
	flag.Parse()
	db, err := store.Open(filepath.Join(*data, "scheduler.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	docker := executor.NewDocker(*socket, "z-task-scheduler", "io.jiuwusan.z-task-scheduler")
	engine := scheduler.New(db, executor.NewHTTP(), docker)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err = engine.Start(ctx); err != nil {
		return err
	}
	server := &http.Server{Addr: *listen, Handler: httpapi.New(db, engine, docker), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	stopped := make(chan error, 1)
	go func() {
		slog.Info("任务调度服务已启动", "listen", *listen)
		stopped <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err = <-stopped:
		if !errors.Is(err, http.ErrServerClosed) {
			stop()
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	httpErr := server.Shutdown(shutdown)
	engineErr := engine.Close(shutdown)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	if httpErr != nil {
		return httpErr
	}
	return engineErr
}
