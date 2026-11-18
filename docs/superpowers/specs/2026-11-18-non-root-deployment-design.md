# 非固定前缀部署设计

用户已确认沿用 convert 的方案：应用无需固定前缀，由外部 Nginx 剥离路径；移除 Origin 限制并更新部署文档。

## 方案与范围

- HTML 的样式、脚本、图标和品牌首页链接均使用相对地址；app.js 在执行时从 `document.currentScript.src` 固定公开目录，后续 API 沿用该目录。
- Nginx 为裸入口补末尾斜杠，带 `/` 的 proxy_pass 将部署前缀替换为 `/`。根路径和单级/多级子目录均可访问，不增加 BASE_PATH、CLI 参数或构建流程。
- 现有 manifest 的 start_url、scope 和图标继续使用相对地址，安装身份保持在公开部署目录。
- 删除 sameOrigin 及所有调用，包括 Origin/Host 比较和 Sec-Fetch-Site 限制。初始化仍需 setup Cookie 和 CSRF，管理写操作仍需有效登录会话和 CSRF；账号密码、限流及错误状态保持原样。
- 保留 scheduler_setup、scheduler_session 名称和后端 Path=/；Nginx 将两个 Cookie 路径统一改为公开目录。HTTPS 反代通过 X-Forwarded-Proto=https 使 Cookie 带 Secure，直接 TLS 行为保持兼容。
- 不改变任务、调度、执行器、SQLite schema、Docker 权限和容器健康检查，不访问真实任务或 Docker，不自动提交、推送或部署。

## 取舍

采用相对资源 + Nginx 剥离前缀；固定 BASE_PATH 需要重复维护配置，X-Forwarded-Prefix 则需要额外代理头和按请求确定前缀，均不采用。该方案要求 Nginx 按约定剥离前缀，不自动兼容保留任意前缀的转发。

## 验收

1. 先失败回归，再实现：根目录及多级目录的实际 HTML 引用可解析到公开目录，安装资源 GET/HEAD、manifest scope 正确。
2. Origin 与内部 Host 不同、Origin 为 null/异常格式、Sec-Fetch-Site=cross-site 时，不因来源拒绝；初始化和管理写操作缺少/错误 CSRF、无会话、错误密码仍被拒绝。
3. 本地代理验证初始化、登录、Cookie 路径、管理读写和退出；任务保持禁用或仅针对本地假上游，不执行真实外部动作。
4. 现有真实进程浏览器套件运行于根路径；新增代理模式运行同一套件于多级目录，验证品牌链接、刷新、资源/API 路径及退出。
5. 执行 go run ./tools/check，运行浏览器门禁；真实 Nginx、容器和 NAS 验证若未运行，交付明确说明。
