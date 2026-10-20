# 首版实施与验证记录

> 后续变更：用户已确认将完整 curl 命令模式替换为 url / method / headers / data HTTP 配置。下文为首版历史设计或验证记录；当前行为与兼容说明以 README 的 HTTP 请求章节为准。

日期：2026-10-20。实施分支：`feat/task-scheduler`，起点：`3f57959`。

## 已交付

中文管理界面、首次创建管理员及登录、curl / Docker 两类任务、Cron / 固定间隔 / 单次调度、手动执行、互斥与超时、SQLite 持久化及执行历史、Dockerfile、威联通 Compose 和 GitHub Actions → Docker Hub 发布流程。

## 本地验证

| 检查 | 结果 |
| --- | --- |
| `go test -count=1 ./...` | 通过，36 个顶层测试用例，含真实本地 curl HTTP 测试及参数子场景 |
| `go vet ./...` | 通过 |
| `gofmt -l cmd internal` | 无未格式化文件 |
| Windows amd64 构建 | 通过 |
| `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build` | 通过，产物为 ELF x86-64 |
| Edge / Playwright 浏览器流程 | 通过，覆盖初始化、登录、多行 curl、创建/编辑/暂停/删除、立即执行、慢请求详情更新、历史和移动布局 |
| JS 语法及 Compose / Actions YAML 解析 | 通过 |
| Docker 镜像、Unix socket 及 Linux race | 本机无 Docker / Linux 工具链，尚未在本地执行；已设为 Actions 发布前门禁 |
| 威联通实机验收 | 尚未执行，需要目标 NAS 的实际 socket 路径、GID、卷权限和 Container Station 环境 |
| GitHub Actions / Docker Hub 发布 | 本轮尚未推送，未观察远端构建，不能声明镜像已发布 |

浏览器截图位于 Git 忽略的 `.test-artifacts/dashboard.png` 和 `.test-artifacts/mobile.png`。浏览器测试使用临时数据库及回环接口，没有连接或修改 NAS。

## 独立审查与修复

独立只读审查覆盖 `3f57959..3dcd593`。审查未发现 Critical，发现的 Important 已通过回归测试修复：

- curl 先在 stdout/stderr 各自流式遮盖凭据，再截断存储，防止输出边界泄露半截密钥。
- 识别紧凑/组合短参数及长参数认证值；区分选项和字面请求体，拒绝 `-:` 操作分组和 `--stderr` 元数据重定向。
- 标准化 `-f` / `-fsS`，避免 Bookworm curl 7.88 与服务的 `--fail-with-body` 冲突；CI 实际镜像测试覆盖这些命令。
- Docker 断连时仍可暂停现有任务；创建、更换目标、重新启用仍校验目标。
- 将 Docker 名称/标签匹配到当前容器默认 hostname，无法确认当前实例时拒绝写操作。
- 登录在 bcrypt 前原子预留尝试额度，限制并发请求绕过失败次数限制。

下列审查项按实际用户影响提升为必要修复，并已完成：脱敏执行快照持久化、低频任务保留最近结果、单次过期历史可见，以及 UTF-8 修复后仍遵守 64 KiB 上限。另外修复了 Docker 客户端固定 10 秒超时提前截断任务配置的问题。每个行为缺陷均先复现失败，再修改实现并通过全量测试。

保留的次要项：curl 使用 `-o` / `-O` 保存响应正文后，详情没有额外的“已写入文件”说明；文件写入由 curl 正常执行，可按命令中的路径查看。

## 实施取舍

- 在当前目录建立功能分支，未另建 worktree；便于沿用用户当前项目，但文件系统隔离程度低于独立 worktree。
- 不支持 curl 配置文件加载和多组操作参数，以保护统一的执行结果采集；这些高级命令需要未来另行设计兼容。
- 本地缺少 Docker，真实镜像和 socket 验证放入 CI 强制门禁；镜像验证结果必须以后续实际 Actions 为准。
- 完整 curl 是受信任管理员执行能力，可访问显式允许的文件、socket，并可使用命令中显式指定的重试；不是不可信脚本沙箱。
- 保持单实例调度和故障后避免重放的策略；跨数据库与外部操作不保证恰好一次，存储故障可能丢失一次触发并记录服务错误。

## 发布前准备

按 README 配置 Docker Hub 仓库、GitHub Actions 用户名与令牌 Secret。合并并推送到 `master` 后，观察 Actions 的 Go、浏览器和实际镜像门禁，再在 NAS 导入 Compose。不要在镜像尚未成功发布时把本地构建通过等同于可以拉取 `latest`。
