# Non-root Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 按用户已确认的 convert 方案支持非固定前缀部署，移除 Origin 限制并提供 Nginx 文档。

**Architecture:** 相对 HTML 资源 + 脚本公开目录生成 API；Nginx 剥离前缀和改写 Cookie Path。HTTP 移除来源限制，保留现有凭据、会话、初始化/管理 CSRF 和限流。

**Tech Stack:** 现有 Go/net/http/embed、原生 JS、SQLite；测试使用 httptest、Playwright 和本地假上游，不新增生产依赖。

**Spec:** `docs/superpowers/specs/2026-11-18-non-root-deployment-design.md`

## Global Constraints

- 当前分支执行，不创建分支或 worktree，不自动提交、推送、部署。
- 保持 HTTP/CLI、任务调度/执行、SQLite 和 Docker 合约。
- 测试使用临时目录和假上游/无效 Docker socket，不读取生产数据或凭据。

## Review Focus

- 所有业务资源和品牌链接必须保留单级/多级公开目录。
- 去掉来源校验后，初始化 CSRF、管理 CSRF、认证和限流仍有效。
- 两类 Cookie 登录/初始化/清除时均正确改写 Path，HTTPS 反代带 Secure。
- 浏览器刷新和安装 start_url/scope 不跳到根目录。
- Nginx 示例保留公网端口、正确剥离前缀，与容器端口/healthcheck 一致。

### Task 1: HTTP 来源限制与 Cookie 代理行为

**Files:** `internal/httpapi/server.go`、`auth.go`、`server_test.go`、新增 `proxy_test.go`

**Interfaces:** 保留 New 与全部 API；删除 sameOrigin；Cookie 的 Secure 同时接受直接 TLS 和 X-Forwarded-Proto=https；测试代理用 httputil.ReverseProxy 剥离前缀并改写两个 Cookie。

- [x] 添加不同 Origin/Sec-Fetch-Site 下初始化、登录、错误凭据、会话、CSRF 拒绝与管理写成功测试，观察失败。
- [x] 更新原来源拒绝测试为缺少 CSRF 拒绝，增加 TLS/代理 HTTPS Cookie 测试，观察失败。
- [x] 删除来源校验、补 Secure，运行 `go test -p 2 ./internal/httpapi`，期望通过。

### Task 2: 资源、代理和真实浏览器

**Files:** `internal/web/index.html`、`app.js`、`internal/httpapi/proxy_test.go`、`test/browser.mjs`

**Interfaces:** HTML 相对引用；app.js 启动时捕获公开目录；browser.mjs 的 TEST_BASE_PATH 仅供本地测试代理使用，不是应用配置。

- [x] 用代理解析并请求实际 HTML 中的业务/安装资源、manifest 和品牌链接，覆盖根/单级/多级路径、GET/HEAD，先观察失败。
- [x] 修改 HTML 和 app.js；补浏览器代理模式，保存实际资源/API 路径、Cookie Path 和无浏览器错误断言。
- [x] 保留现有完整浏览器操作；测试进程显式使用临时缺失的 Docker socket，确保不访问真实 Docker。
- [x] Go 包测试通过后构建 bin/scheduler.exe，根路径和多级目录均运行 `node test/browser.mjs`，期望全部通过。

### Task 3: Nginx 文档及交付

**Files:** `README.md`、`docs/architecture.md`、`docs/development.md`、`.github/workflows/docker-publish.yml`

- [x] 更新根/子目录完整 Nginx 示例、末尾斜杠、端口、Cookie、HTTPS 和部署检查步骤，删除业务未支持子目录及旧来源校验说明。
- [x] CI 在根路径浏览器门禁后运行同一套多级目录流程，发布依赖保持不变。
- [x] gofmt 仅格式化本次 Go 文件，执行 `go run ./tools/check`，期望退出码 0。
- [x] 对最终改动做独立审查，报告真实验证和未验证的 Nginx/Docker/NAS 边界。

## Execution notes

- 用户已经选定方案并明确要求实现，在当前会话直接执行。
- 任务 2 的代理集成测试使用任务 1 的真实初始化/认证流程；两个任务一并交付。

## 验证记录

- Go RED：实际 HTML 主资源越出子目录、Origin/Sec-Fetch-Site 拦截初始化、HTTPS 代理 Cookie 缺少 Secure，新增测试均先失败。
- 浏览器 RED：保留旧 API 根路径时，多级目录无法完成初始化；自动定位 API 目录后通过。
- go run ./tools/check 通过：gofmt、go vet、全部 Go 测试与架构检查、全部包构建、14 项安装交互及 2 项浏览器启动保护测试。
- 真实浏览器两轮通过：根路径和 TEST_BASE_PATH=/tools/scheduler；使用 Playwright + 本机 Edge、临时数据库、本地假上游及无效 Docker socket。
- 真实进程覆盖初始化/登录/退出、品牌链接、刷新、安装作用域、资源/API 地址、HTTP 任务配置/执行/历史、移动端及 Docker 选择假 API。截图按两种模式分别归档。
- 独立审查：生产代码无实质缺陷；修正测试端口占用与早退清理（先失败后通过），同时修正假代理 Cookie Path 改写和截图覆盖问题。测试启动保护加入统一门禁。
- Linux amd64 / CGO_ENABLED=0 交叉编译通过。未运行真实 Nginx、Docker 引擎/镜像或 NAS 验收。
- 当前分支保留改动，不自动提交、推送或部署。
