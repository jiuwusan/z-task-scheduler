# z-task-scheduler

面向个人 NAS 的中文定时任务管理器。通过 URL、method、headers、data 配置 HTTP 请求，并支持 Docker 容器的启动、停止和重启。HTTP 请求由 Go 原生客户端发送，无需 curl 程序。Go 单进程与内嵌 Web 界面，SQLite 持久化任务和执行记录。目标设备为威联通 TS-464C2（linux/amd64）。

## 功能

- 任务创建、编辑、启用、暂停、删除和立即执行。
- 五字段 Cron、固定间隔（至少 60 秒）、指定时间执行一次。
- 默认上海时区，显示下一次执行时间。
- 同一任务和同一 Docker 容器互斥，全局最多并发 4 个任务。
- 默认超时 60 秒，可配置 1–3600 秒；失败不自动重试。
- 执行历史保留 30 天，单次输出最多保存 64 KiB；正在查看的执行详情会自动更新结果。
- 首次运行创建管理员，密码哈希存储，登录会话及 CSRF 保护。

## GitHub 构建与 Docker Hub

工作流位于 `.github/workflows/docker-publish.yml`，参考 `z-media-workflow` 的发布方式。

1. 在 Docker Hub 创建 `jiuwusan/z-task-scheduler` 仓库。
2. 在 GitHub 仓库 Settings → Secrets and variables → Actions 设置：
   - `OCKERHUB_USERNAME` 或 `DOCKERHUB_USERNAME`：有该镜像仓库推送权限的用户名；同时存在时优先使用前者，沿用原项目约定。
   - `DOCKERHUB_TOKEN`：有目标仓库写权限的 Docker Hub 访问令牌。
3. 推送到 `master`，或在 Actions 手动运行工作流。

GitHub 先执行 Go 静态检查、竞态测试和 Chromium 浏览器测试，再构建实际镜像，在隔离的 CI Docker 主机中验证 HTTP 请求、Unix socket、容器启停、自身保护和重建后的持久化，验证通过才发布。

`master` 发布 `latest`、`master` 和 `sha-提交短哈希`。手动构建其他分支发布对应分支和提交标签，不覆盖 `latest`。PR 只测试和构建，不登录或推送 Docker Hub。新仓库需要自行配置上述 Secret，原仓库的 Secret 不会自动复制。

## 威联通部署

NAS 无需安装 Go。先在 Container Station 安装并启动 Docker 环境，然后使用项目的 `docker-compose.yml`。

### 检查 Docker socket

在 NAS SSH 终端执行：

```sh
ls -l /var/run/docker.sock
stat -c '%g' /var/run/docker.sock
```

第二条输出 socket 的数字组 ID。Compose 的 `group_add` 必须包含这个 GID，默认是 `0`。

- 使用 Container Station「应用程序 → 创建」粘贴 YAML 时，如果 GID 不是 0，直接将 `group_add` 的值改成实际数字，例如 `- '999'`。
- 使用 SSH Compose 时，可在项目目录的 `.env` 写入 `DOCKER_SOCKET_GID=实际数字`，或直接改 YAML。该变量仅用于 Compose 组权限配置，不是应用凭据。
- 如果实际 socket 不是 `/var/run/docker.sock`，将挂载源路径改为真实路径，容器目标仍为 `/var/run/docker.sock`。

程序以 UID/GID `10001:10001` 运行，通过附加组访问 socket，无需 `privileged`，不要将 socket 改为全员可写。访问 socket 具有高权限，管理面板应放在可信内网或受保护的访问入口。

### 启动

镜像首次发布成功后，在 Container Station 导入 Compose，或通过 SSH 执行：

```sh
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose ps
docker compose logs --tail=100
```

打开 `http://NAS地址:3010`，创建管理员后登录。密码为 8–72 字节。

Compose 默认使用 `scheduler-data` 命名卷挂载 `/app/data`，更新镜像和重建容器保留任务、账号和历史。NAS 重启后的自动运行依赖 Container Station 本身启动和容器的 `restart: always` 策略。

若需要将数据放在可见共享文件夹，可以把 `scheduler-data:/app/data` 改为 `/share/你的共享目录/z-task-scheduler:/app/data`。先创建该目录并赋予 UID 10001 写权限；同时确认 QNAP 共享文件夹 ACL 没有阻止写入。备份或拷贝 SQLite 数据时先停止服务，连同数据库及 WAL 文件一起备份整个数据目录。

容器名称 `z-task-scheduler`、标签 `io.jiuwusan.z-task-scheduler: 'true'` 和 Docker 默认的容器 ID hostname 用于共同识别调度器自身，请保留，不要自定义 `hostname`。身份识别失败时 Docker 写操作会关闭，HTTP 任务仍可运行。

### 升级与回退

```sh
docker compose pull
docker compose up -d
```

需要固定或回退版本时，将 `image` 改成已经成功发布的 `jiuwusan/z-task-scheduler:sha-提交短哈希`，再执行上述命令。备份数据后再升级；首版没有自动降级数据库迁移功能。

停止服务可用 `docker compose stop`；需要移除容器但保留数据时用 `docker compose down`。不要加 `-v`，它会删除持久卷。

## HTTP 请求任务

界面使用 URL、请求方法、请求头键值对和请求体四项配置，例如任务 API 中的请求部分为：

```json
{
  "type": "http",
  "url": "http://192.168.1.100:3000/api/example",
  "method": "POST",
  "headers": {
    "Authorization": "Bearer 替换为实际令牌"
  },
  "data": {
    "enabled": true
  }
}
```

- method 默认为 GET，支持 GET、POST、PUT、PATCH、DELETE、HEAD、OPTIONS。
- headers 可为空，通过界面增加或删除键值对；名称不区分大小写，不能重复。Content-Length 和 Transfer-Encoding 由客户端管理。
- data 可省略或为 null。对象、数组作为 JSON 发送，自动补充 application/json；字符串按原文发送，可自行指定 Content-Type。
- 界面的请求体提供 JSON 和原始文本两种格式；查询参数直接写入 URL。
- JSON 请求体在创建、编辑、启停时保留数字原文，避免超大整数被浏览器舍入。
- 使用 Go net/http 直接请求，仅接受 HTTP/HTTPS URL，默认不跟随重定向、不配置自动重试，遵守任务超时。
- HTTP 4xx/5xx 记为失败并保留响应正文；3xx 保留原状态码和正文，不访问重定向目标。网络错误、读响应失败及超时有相应执行结果。
- localhost 指调度器容器自身。调用其他服务时使用 NAS 地址及映射端口，或将调度器接入目标容器所在网络。
- 响应正文记录最多 64 KiB。已识别的请求头、Cookie、URL 认证/查询参数和请求体敏感字段会在截断前遮盖；未知内容或任意编码形式仍需由管理员判断。
- 列表和历史不返回原始 headers、data 或 URL 中已识别的凭据；管理员编辑任务时能读取完整配置。

### 旧 curl 任务

完整命令输入模式已替换。升级时旧任务保留原命令并自动暂停；在任务列表点击“改为 HTTP”，编辑器会显示原命令供参考，手动填写四项 HTTP 配置后保存。不会自动转换或继续执行旧命令，避免请求含义发生变化。历史记录继续保留。

## 调度与 Docker 行为

Cron 为 `分 时 日 月 周`，如 `0 8 * * *` 表示每天 08:00。Cron 按任务时区计算；单次执行由浏览器本地时间转换为明确时刻。间隔按计划时间推进，不等待上一次结束再重新计时。

暂停任务后仍可手动执行。Docker 断连时也可以暂停已有任务；创建、重新启用或更换容器目标时需通过在线检查。任务正在执行时，编辑只影响下一次运行；删除会被拒绝。并发满时，定时触发记为跳过，手动执行返回繁忙。重启后，未完成记录标为中断，错过的定时触发跳过；过期单次任务关闭并记录“已过期”，不自动补执行。历史保留脱敏后的目标、操作、超时和调度快照。单实例运行，不提供跨进程的恰好执行一次保证。

Docker 任务按容器名称绑定，每次执行先查询该名称当前的完整容器 ID，再通过 ID 调用 Docker 引擎的 start、stop 或 restart 接口。执行 `docker compose down` 后再 `up -d`，只要容器名称保持一致，任务无需重新配置。名称不存在时本次记为失败，后续正常触发仍会重新查询；如果容器改名，需要编辑任务重新选择。Compose 建议配置固定的 `container_name`，或保持项目名和服务名不变。相同名称的任务互斥，自身保护在名称解析后按实际 ID 再检查。

旧任务已有 `containerName` 的直接沿用，旧 `containerId` 不再参与目标定位；仅通过旧 API 保存 ID、未保存名称的任务，需要重新选择容器。新增 API 任务使用 `containerName`，无需提交 `containerId`。

已启动容器再次 start、已停止容器再次 stop 均成功并提示无需操作。成功表示引擎完成操作，不保证容器内应用已健康；Docker API 超时会标记结果未知，需要检查容器实际状态，服务不会自动重发。名称解析后如果容器恰好又被删除，本次请求会失败，不在本次运行中切换目标或自动重发。

服务状态页会显示 socket 连接及自身识别错误。遇到权限问题请检查挂载路径、宿主机 socket GID、`group_add` 和数据卷写权限，不要开启整个容器的特权模式。

## 本地开发

需要 Go 1.27.3。无需本地 Docker即可运行 Go/API 测试和管理界面。

```powershell
go mod download
go test ./...
go vet ./...
go run ./cmd/scheduler -listen 127.0.0.1:3010 -data-dir ./data
```

打开 `http://localhost:3010`。Windows 没有 NAS Unix socket 时，Docker 显示未连接是正常状态，HTTP 请求功能可以测试。命令行支持 `-listen`、`-data-dir`、`-docker-socket`；进程收到停止信号后限时取消任务并保存结果。服务重启后需要重新登录。

浏览器测试需要 Node.js 22 和 Playwright，可在忽略目录安装测试工具：

```powershell
go build -o bin/scheduler.exe ./cmd/scheduler
npm install --prefix .test-artifacts/browser-tools --no-save --no-package-lock playwright@1.63.0
$env:PLAYWRIGHT_MODULE = "$PWD/.test-artifacts/browser-tools/node_modules/playwright"
$env:PLAYWRIGHT_CHROMIUM_EXECUTABLE = 'C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe'
node test/browser.mjs
```

Linux CI 设置 `SCHEDULER_BINARY=bin/scheduler`，使用 Playwright 自带 Chromium。浏览器测试会创建临时数据库及本地模拟接口，并保存桌面/手机截图到 `.test-artifacts`。`test/image-smoke.mjs` 仅在隔离的 Linux Docker 环境运行，它创建并清理专属测试容器和数据卷；发现已有同名调度器会拒绝替换。

## 验证范围

本地验证包括 Windows Go/API 测试、真实 HTTP 请求、浏览器操作及 Linux amd64 交叉编译。实际镜像、Linux 竞态和 Unix socket 集成测试由 GitHub Actions 执行。NAS 的 socket 路径、组权限、Container Station 导入和实际任务行为，仍需在目标设备验收。只有观察到 Actions 成功后，才能确认对应镜像已经发布。
