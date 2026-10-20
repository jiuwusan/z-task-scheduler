# Task Scheduler Implementation Plan

> 后续变更：用户已确认将完整 curl 命令模式替换为 url / method / headers / data HTTP 配置。下文为首版历史设计或验证记录；当前行为与兼容说明以 README 的 HTTP 请求章节为准。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 在威联通 TS-464C2 上以单容器运行中文定时任务管理器，支持完整 curl 命令和 Docker start/stop/restart。

**Architecture:** Go 单进程提供 API、内嵌静态界面、调度器和执行器。SQLite 持久化配置、任务和执行历史，Docker API 经 Unix socket 访问，curl 通过独立子进程执行。GitHub Actions 测试并发布 Docker Hub 镜像。

**Tech Stack:** Go 1.27.3、标准库 net/http/database/sql/embed、modernc.org/sqlite v1.60.1、github.com/robfig/cron/v3 v3.0.1、golang.org/x/crypto v0.58.0、原生 HTML/CSS/JavaScript、curl、Docker Compose。依赖版本已于 2026-10-20 查询 Go module proxy；初始化时写入 go.mod/go.sum。

**Spec:** `docs/superpowers/specs/2026-10-20-task-scheduler-design.md`

## Global Constraints

- `jiuwusan/z-task-scheduler`，首版构建 `linux/amd64`；默认上海时区 `Asia/Shanghai`。
- 五字段 Cron 不含秒；固定间隔最短 60 秒；支持指定时间执行一次。
- 默认超时 60 秒，可在 1 至 3600 秒之间调整；全局最多同时执行 4 个任务。
- 同一任务只允许一个执行实例；同一容器的不同 Docker 任务也互斥。
- 停机期间错过的触发跳过；首版失败后不自动重试。
- 每次执行保存的输出最多 64 KiB；历史默认保留 30 天。
- `/app/data` 持久化；默认映射 `3010:3000`；默认 socket `/var/run/docker.sock`；`restart: always`。
- 完整 curl 参数直接启动子进程，不调用 Shell；禁用隐式 curl 默认配置；不接受用户自定义 `-w`/`--write-out`。
- Docker 以容器 ID 保存目标，禁止操作自身；请求结果未知不自动重发。
- 管理接口要求登录，写操作要求 CSRF；原命令仅管理员编辑接口返回，输出和历史脱敏。
- 所有 Go 测试须能在 Windows 上执行，Unix socket/镜像集成补充验证在 Linux CI 执行。

## Review Focus

1. curl JSON、URL 查询符和多行引号不能被错误分词；日志截断边界不能泄露半截密钥（Task 3）。
2. 重启遇到过期单次任务和运行中记录，不能重复执行；手动与定时同时触发仍互斥（Tasks 2、5）。
3. 容器被同名重建、自己无法识别或 Docker 连接超时，应明确拒绝或显示未知结果（Task 4）。
4. 两个浏览器同时初始化及过期会话、跨站写入，不能绕过管理员保护（Task 6）。
5. NAS 数据目录不可写、socket 组权限不匹配、镜像缺少 curl，必须给出可诊断结果（Tasks 7、8）。

## File Structure

| 路径 | 职责 |
| --- | --- |
| `cmd/scheduler/main.go` | 参数、启动、信号及优雅退出 |
| `internal/domain/{task,run}.go` | 共享任务和执行结果类型 |
| `internal/schedule/rule.go` | 校验时间规则和计算下一次执行 |
| `internal/store/{store,migrations,tasks,runs,settings}.go` | SQLite、任务事务、历史和管理员持久化 |
| `internal/executor/{executor,curl_parse,curl,output,docker}.go` | 统一执行接口、curl 分词/进程、限量脱敏输出、Docker HTTP 客户端 |
| `internal/scheduler/engine.go` | Tick、互斥、容量、调度更新、关闭 |
| `internal/auth/{password,session}.go` | 密码哈希、会话、CSRF 和登录限流 |
| `internal/httpapi/{server,auth,tasks,runs,docker}.go` | HTTP 路由、校验、API 与错误映射 |
| `internal/web/{embed.go,index.html,app.js,style.css}` | 内嵌中文管理面板 |
| 各包 `*_test.go`、`test/browser.mjs` | 行为测试、API 集成及浏览器冒烟 |
| `Dockerfile`、`.dockerignore`、`docker-compose.yml` | 构建与 NAS 部署 |
| `.github/workflows/docker-publish.yml` | 测试、镜像验证与发布 |
| `README.md` | 开发、Secret、部署、升级及排错 |

## Task 1: 任务模型和时间规则

**Files:** Create `go.mod`, `go.sum`, `internal/domain/task.go`, `internal/domain/run.go`, `internal/schedule/rule.go`, `internal/schedule/rule_test.go`; modify `.gitignore` to exclude local data, binaries and browser artifacts.

**Interfaces:** Define `domain.Task` with `ID, Name, Type, CurlCommand, ContainerID, ContainerName, DockerAction string`, `Enabled bool`, `TimeoutSeconds int`, `Schedule domain.Schedule`, `NextRunAt *time.Time`; `domain.Schedule` has `Kind, Cron, Timezone string`, `IntervalSeconds int`, `At *time.Time`. Define `domain.Run` with `ID, TaskID, TaskName, Trigger, State string`, `Snapshot Task` (internal-only, exclude raw command from serialized history), `StartedAt time.Time`, `FinishedAt *time.Time`, `Result RunResult`. `domain.RunResult` has `State, Output, Error string`, `ExitCode, HTTPStatus int`, `Truncated bool`; states are `running`, `success`, `failed`, `timeout`, `unknown`, `skipped`, `interrupted`. Produce `domain.ValidateTask(t *Task) error` (apply timeout 60 default), `schedule.Validate(rule domain.Schedule, now time.Time) error`, `schedule.Next(rule domain.Schedule, after time.Time) (*time.Time, error)`, `schedule.Advance(rule domain.Schedule, previous, now time.Time) (*time.Time, error)`; single-run completion is managed by the engine.

- [x] Write `TestFiveFieldCron`, `TestIntervalAndTimeoutBounds`, `TestSingleRunRequiresFuture`, `TestInvalidTimezoneAndDST`. Assert Shanghai `0 8 * * *` advances to the next local 08:00; 59 seconds is invalid and 60 valid; six-field Cron is invalid; DST advancement is strictly forward. Add common task validation for types, names and timeout 1..3600 with 60 default.

```go
if err := schedule.Validate(domain.Schedule{Kind: "interval", IntervalSeconds: 59, Timezone: "Asia/Shanghai"}, time.Now()); err == nil { t.Fatal("59-second interval accepted") }
if err := schedule.Validate(domain.Schedule{Kind: "interval", IntervalSeconds: 60, Timezone: "Asia/Shanghai"}, time.Now()); err != nil { t.Fatal(err) }
```

- [x] Run `go test ./internal/schedule` and confirm missing production symbols fail. Initialize module `github.com/jiuwusan/z-task-scheduler` and pin cron dependency as necessary to run the test harness.
- [x] Implement shared models and schedule functions, loading embedded timezone data for consistent Windows/Linux behavior. `Advance` skips interval points already passed rather than using completion time as a new anchor.
- [x] Run `go test ./internal/schedule`, `go vet ./internal/schedule`; require exit 0.
- [x] Commit `feat: add task models and scheduling rules`.

## Task 2: SQLite 存储与重启恢复

**Files:** Create `internal/store/store.go`, `migrations.go`, `tasks.go`, `runs.go`, `settings.go`, `store_test.go`.

**Interfaces:** `store.Open(path string) (*Store,error)`, `(*Store).Close() error`; `CreateTask(ctx context.Context,t domain.Task) (domain.Task,error)`, `UpdateTask(ctx context.Context,t domain.Task) error`, `GetTask(ctx context.Context,id string) (domain.Task,error)`, `ListTasks(ctx context.Context) ([]domain.Task,error)`, `DeleteTask(ctx context.Context,id string) error`; `CreateRun(ctx context.Context,r domain.Run) (domain.Run,error)`, `UpdateRun(ctx context.Context,r domain.Run) error`, `ListRuns(ctx context.Context,taskID string,page,limit int) ([]domain.Run,int,error)`, `GetRun(ctx context.Context,id string) (domain.Run,error)`, `Recover(ctx context.Context,now time.Time) error`, `Prune(ctx context.Context,before time.Time) error`; admin `InitializeAdmin(ctx context.Context,username,hash string) error`, `Admin(ctx context.Context) (string,string,error)`.

- [x] Write temporary-database tests for task round trip across reopen, duplicate initialization rejection, unfinished runs becoming interrupted, expired single tasks disabled, deleted task history preserved, and pruning records older than 30 days.
- [x] Run `go test ./internal/store`; confirm failure before implementation.
- [x] Add pure Go SQLite driver; use migrations, parameterized SQL, bounded connections and explicit transactions for initialization/recovery. Keep database directory owner-only where supported. Fail startup with contextual error if directory/database cannot be opened. Use stable IDs and UTC persisted timestamps.
- [x] Run `go test ./internal/store ./internal/schedule`; require persistence/recovery tests passing, including a failed transaction leaving no partial administrator.
- [x] Commit `feat: persist tasks and execution history`.

## Task 3: curl 参数与执行器

**Files:** Create `internal/executor/executor.go`, `curl_parse.go`, `curl.go`, `output.go`, `curl_parse_test.go`, `curl_test.go`, `output_test.go`.

**Interfaces:** `executor.Executor` exposes `Execute(ctx context.Context,task domain.Task) domain.RunResult`, consuming Task 1 models. `executor.ParseCurl(command string) ([]string,error)`, `executor.NewCurl(binary string) *Curl`, `(*Curl).Execute(ctx context.Context,task domain.Task) domain.RunResult`.

- [x] Write parser tests asserting exact argv for JSON, quoted query `?a=1&b=2`, escaped double quotes, empty arguments and multi-line `\` continuation. Reject unclosed quotes, Shell operator tokens, `-w`/`--write-out` including attached forms, config-file loading and `--next` that could override managed metadata. Quoted Shell syntax remains literal.

```go
args, err := executor.ParseCurl(`curl 'http://localhost/?a=1&b=2' --data '{"enabled":true}'`)
if err != nil || !reflect.DeepEqual(args, []string{"http://localhost/?a=1&b=2", "--data", `{"enabled":true}`}) { t.Fatalf("args=%q error=%v", args, err) }
```

- [x] Write local `httptest.Server` integration tests for POST/body/header round trip, HTTP 500 failure with body retained, connection error, cancellation and process exit, 64 KiB output cap, and redaction of header/basic-auth secrets across chunk/truncation boundaries. Run `go test ./internal/executor -run 'Curl|Output'` and observe missing-symbol failures.
- [x] Implement argument parsing without Shell, execute with context cancellation and `Wait`, `-q` as first argument, service-owned random-delimited write-out metadata, and output drainage independent of saved length. Bound stderr and stdout together; never let a discarded body hide metadata. HTTP code extraction failure produces a diagnostic instead of invented success. Resolve `curl.exe` on Windows; require curl on Linux. Redact extracted credentials before exposing bounded output.
- [x] Run `go test ./internal/executor -run 'Curl|Output'`; tests call the real local curl against loopback endpoints, with no NAS changes.
- [x] Commit `feat: execute pasted curl commands`.

## Task 4: Docker API 与自身保护

**Files:** Create `internal/executor/docker.go`, `docker_test.go`.

**Interfaces:** `executor.Container` has `ID, Name, State string`, `Self bool`; `executor.NewDocker(socket,selfName,selfLabel string) *Docker`, `(*Docker).List(ctx context.Context) ([]Container,error)`, `(*Docker).Status(ctx context.Context) error`, `(*Docker).ValidateTarget(ctx context.Context,id string) error`, `(*Docker).Execute(ctx context.Context,task domain.Task) domain.RunResult`. Internal constructor accepts an HTTP transport for mock tests; production transport dials the configured Unix socket.

- [x] Write mocked-engine tests for API version negotiation via unversioned `/version`, list including stopped containers, correct start/stop/restart URLs, already-started/stopped success, missing ID after same-name replacement, self target rejection, label mismatch disabling writes, and timeout returning unknown with no retry.
- [x] Run `go test ./internal/executor -run Docker`; confirm failure before implementation.
- [x] Implement a narrow Docker HTTP client with negotiated API version, request contexts and bounded errors. Identify self by fixed Compose name plus label `io.jiuwusan.z-task-scheduler=true`, checking inspected ID against the self candidate; never silently enable writes if identification fails. Stop grace must fit inside task deadline. Refresh/revalidate identity and target before writes. Keep Docker unavailable as a recoverable service status.
- [x] Run `go test ./internal/executor`; require curl and Docker tests passing independently of a local Docker installation.
- [x] Commit `feat: control Docker containers with self protection`.

## Task 5: 调度、互斥和关闭

**Files:** Create `internal/scheduler/engine.go`, `engine_test.go`.

**Interfaces:** `scheduler.New(db *store.Store,curl,docker executor.Executor) *Engine`, `(*Engine).Start(ctx context.Context) error`, `(*Engine).Tick(ctx context.Context,now time.Time) error`, `(*Engine).RunNow(ctx context.Context,id string) (domain.Run,error)`, `(*Engine).SaveTask(ctx context.Context,t domain.Task,now time.Time) (domain.Task,error)`, `(*Engine).DeleteTask(ctx context.Context,id string) error`, `(*Engine).Close(ctx context.Context) error`. All API task writes go through the engine for consistency with dispatch; executor tests use blocking fakes.

- [x] Write controlled-time tests for each schedule, overdue skipping, edited running task preserving its snapshot, paused tasks not auto-dispatched, manual execution sharing task locks, two task IDs targeting one container, four-run global limit, skip records versus manual busy error, and deleting a running task returning conflict.
- [x] Add one-shot tests for completion/expiration and shutdown cancellation, plus failure not auto-retried; run `go test ./internal/scheduler` and observe expected failures.
- [x] Implement dispatch under one state lock: reserve task/container/capacity, persist running record and advance next schedule before launching a worker; release on every exit including panic. Persist final results; don't claim success on storage failure. Tick runs every second, history pruning daily; no unbounded queue. `Close` prevents new work, cancels workers and waits within its context.
- [x] Run `go test ./internal/scheduler ./internal/store ./internal/executor`; also run `go test -race ./internal/scheduler` on Linux CI, or locally only if a race-compatible C toolchain is available.
- [x] Commit `feat: schedule tasks and prevent overlapping runs`.

## Task 6: 登录与管理 API

**Files:** Create `internal/auth/password.go`, `session.go`, `auth_test.go`; `internal/httpapi/server.go`, `auth.go`, `tasks.go`, `runs.go`, `docker.go`, `server_test.go`.

**Interfaces:** `auth.Hash(password string) (string,error)`, `auth.Verify(hash,password string) bool`, `auth.NewSessions() *Sessions`; `auth.Session` has `Token, CSRF, Username string`, `ExpiresAt time.Time`; `(*Sessions).Create(username string,now time.Time) (Session,error)`, `(*Sessions).Get(token string,now time.Time) (Session,bool)`, `(*Sessions).Delete(token string)`; tokens are random, sessions last 24h. `httpapi.New(db *store.Store,engine *scheduler.Engine,docker *executor.Docker) http.Handler`. Responses are `{data:...}` or `{error:{code,message}}`. Ports and data paths are runtime options, not browser-editable network controls.

- [x] Write API tests for first setup, password minimum 8 and bcrypt maximum 72 bytes, concurrent setup only one success, login/logout/session expiry, login throttling after 5 failed attempts per client per 15 minutes, unauthenticated API rejection, absent/invalid CSRF and cross-origin mutation rejection.
- [x] Add tests for task CRUD validation, details-only curl command access, HTTP status for busy/conflict/not-found, paginated history capped at 100, Docker connection errors and healthy `/health` before setup. Run `go test ./internal/auth ./internal/httpapi` to confirm failures.
- [x] Implement bcrypt, HttpOnly/SameSite session cookies, request size limits and sanitized errors. Initialize via a short-lived setup cookie plus CSRF, enforce same-origin mutations using request origin/host, avoid trusting arbitrary proxy headers. Routes: `/api/setup/status`, `/api/setup`, `/api/auth/login`, `/api/auth/logout`, `/api/auth/session`, `/api/tasks`, `/api/tasks/{id}`, `/api/tasks/{id}/run`, `/api/runs`, `/api/runs/{id}`, `/api/docker/containers`, `/api/status`, `/health`.
- [x] Run `go test ./internal/auth ./internal/httpapi`; verify real SQLite plus fake executors through the API, not controller mocks alone.
- [x] Commit `feat: add authenticated task management API`.

## Task 7: 中文管理面板和可运行服务

**Files:** Create `cmd/scheduler/main.go`, `internal/web/embed.go`, `index.html`, `app.js`, `style.css`, `internal/httpapi/web_test.go`, `test/browser.mjs`.

**Interfaces:** `web.Handler() http.Handler` serves embedded assets; main registers API and web routes. Runtime flags `-listen` default `:3000`, `-data-dir` default `./data`, `-docker-socket` default `/var/run/docker.sock`; container overrides data dir to `/app/data`. A `healthcheck` subcommand probes the local health endpoint for Docker HEALTHCHECK.

- [x] Write HTTP tests for embedded index/assets, correct content types, unknown API 404 and healthcheck exit behavior. Write browser assertions for first setup/login, create multiline curl, edit/pause/run/delete, schedule help and next-run display, Docker-unavailable message, history output/error rendering and mobile layout.
- [x] Run backend tests before main/web integration; run browser smoke once the app is running. UI checks must fail clearly if features are absent. Use existing browser tooling/temporary Playwright runner outside product dependencies.
- [x] Build a concise responsive Chinese interface with sidebar, task filters, type badges, status, editor modal, log detail drawer and account logout. Insert user content with textContent; preserve multiline commands, disable duplicate submits, handle CSRF/session expiry, convert local single-run dates to explicit RFC3339 and retain form state on validation errors.
- [x] Wire main to open data, recover tasks, start scheduler and handle OS interrupts. Shutdown HTTP and execution within 30 seconds. Run `go test ./...`, `go vet ./...`, `go build ./cmd/scheduler`, launch locally and complete browser flow using loopback HTTP mock; record actual browser evidence.
- [x] Commit `feat: add Chinese scheduler dashboard`.

## Task 8: 镜像、GitHub 发布与威联通文档

**Files:** Create `Dockerfile`, `.dockerignore`, `docker-compose.yml`, `.github/workflows/docker-publish.yml`, `README.md`.

**Interfaces:** Image `jiuwusan/z-task-scheduler`; binary at `/app/scheduler`, data `/app/data`, port 3000, Compose maps 3010. Dockerfile compiles with Go 1.27.3 and CGO disabled, runtime contains curl/CA/tzdata and a non-root user. Compose accepts `DOCKER_SOCKET_GID` for group_add; README explains obtaining actual QNAP socket GID and precreating writable bind directories. Fixed self-identification label/name matches Task 4.

- [x] Add Linux integration assertions for image health, runtime UID, writable data, curl availability, restart persistence and Docker lifecycle of a uniquely labeled disposable test container. Assert operations targeting scheduler itself fail. Keep real NAS containers outside CI tests.
- [x] Adapt the reference workflow: setup Go from go.mod, format check, `go vet ./...`, `go test ./...`, Linux race checks, Buildx image build/load for smoke; only after success publish with official Docker actions and GHA cache. PR never logs in or pushes. Test an actual Linux Unix-socket client connection, not only mocked HTTP.
- [x] Implement Compose with named volume, socket, group_add, `restart: always`, 45-second stop grace, bounded Docker logs and the service healthcheck. README provides Container Station import, Docker Hub repository/Secrets setup, initial account creation, curl limits, container-network localhost semantics, upgrade/rollback, data backup, socket permission troubleshooting and stop-without-volume-deletion commands.
- [x] Run local Go tests/vet and `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` with output outside Git. Run Docker build/smoke/Compose config only where Docker is available; mark checks not executed locally clearly. Do not claim remote Actions passed until its run has been observed. Validate workflow/YAML syntax with available parser.
- [x] Commit `ci: publish scheduler image to Docker Hub`.

## Task 9: 全量验收与交付

**Files:** Update `README.md` and this plan with actual verification results and completed checkboxes; fixes stay in the owning module with regression tests.

**Interfaces:** No new product API. Deliver a reviewable diff and exact deployment instructions matching the built artifact.

- [x] Review the full diff against every spec section and Review Focus item, fixing any missing requirements before final verification.
- [x] Run clean `go test ./...`, `go vet ./...`, formatting check, Windows build, Linux amd64 cross-build and browser smoke. Run Linux race/socket/image checks when available; report any unavailable platform checks precisely.
- [x] Inspect `.dockerignore`/`.gitignore` and staged paths to ensure no database, credentials, test output or compiled binaries enter Git or the image context.
- [x] Present current local commits and verification evidence. GitHub push/publishing follows the user's release instruction; don't assume Docker Hub Secrets exist. If requested, first verify the named remote and push only the intended commits; follow Actions to completion and report the exact image tag.
- [x] Provide NAS import/pull/up instructions. NAS acceptance remains outstanding until tested on the actual device; never equate a mock test with NAS success.

## Execution Handoff

建议由当前助手在本聊天按任务顺序直接实施，这些模块共享接口较多，适合连续集成验证。实施前请审阅本计划，并选择当前聊天直接实施或子代理实施。当前项目是干净的普通 checkout；建议直接在当前目录实施，如需 worktree，在创建前征得用户同意。

2026-10-20：已按用户选择在当前聊天直接实施，已完成本地实现、测试、独立审查与回归修复。Linux 镜像、竞态及 NAS 验收须以后续实际 CI 和设备验证为准，完整证据见 `docs/verification-2026-10-20.md`。当前终端继承旧 PATH，实施时使用已经验证的 `D:\develop\Go\bin\go.exe` 或在单次进程中更新 PATH，无需用户重新安装或修改系统变量。

## References

- Go Cron 文档：https://pkg.go.dev/github.com/robfig/cron/v3
- SQLite 驱动文档：https://pkg.go.dev/modernc.org/sqlite
- bcrypt 文档：https://pkg.go.dev/golang.org/x/crypto/bcrypt
- Docker Engine API：https://docs.docker.com/reference/api/engine/
- 参考项目发布文件：`E:/personal/code/z-media-workflow/.github/workflows/docker-publish.yml`
