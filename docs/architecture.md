# 项目架构

本项目保留已建立的 Go 分包架构。新增 service 用例层收拢 HTTP 原来负责的任务/计划校验、Docker 目标校验、类型字段归一化及任务总览/脱敏聚合，不改调度引擎、SQLite 或 HTTP 合约。

## 目录与职责

- cmd/scheduler：配置、SQLite、执行器、调度引擎与 HTTP 组合根，信号取消和关闭。
- internal/httpapi：net/http 路由与控制器、JSON 解码/响应、会话/CSRF/Origin/限流和静态管理页面。New 是保留的组合适配入口，接收现有 store/engine/docker 依赖并注入服务。HTTP 保留简单只读查询和会话编排，任务与管理员初始化/凭据验证交由 service。
- internal/service：任务保存用例（任务和计划校验、停用一次性任务的时间语义、启用 Docker 目标校验、移除异类型字段）、任务列表与最近运行摘要聚合/脱敏。Accounts 用例通过 AccountStore 执行管理员初始化、密码校验和认证。Tasks 依赖 TaskStore/TaskEngine/DockerTarget 端口和可替换时钟，无 HTTP、磁盘或 Docker socket I/O。
- internal/scheduler：调度、任务写入/删除、执行并发/容器互斥、RunNow 与关闭；仍是执行用例服务，不因新增目录重复实现现有引擎。
- internal/executor：HTTP/Docker 出站协议、超时、输出上限和允许容器检查；不负责会话与持久化。
- internal/store：SQLite 事务、迁移、任务/运行/管理员持久化。
- internal/domain：任务/运行 DTO、HTTP/Docker 字段与安全约束纯校验。
- internal/schedule：计划规则、时区和下次运行的纯计算。
- internal/auth：密码散列/验证、内存会话及 CSRF 值，不依赖 HTTP 对象。
- internal/web：原生浏览器页面与 Go embed。
- internal/architecture、tools/check：工程门禁。

## 依赖方向

httpapi → service/scheduler/auth 与查询适配器；service → domain/schedule/auth/store 错误值和注入端口；scheduler → store/executor/schedule/domain；store → domain/schedule；executor/schedule → domain；auth/domain/web 为叶子。HTTP 组合适配入口继续接收具体基础设施，不将 service 反向注入 HTTP 对象。internal/architecture 明确列出允许矩阵并拒绝未知层；domain/service 禁止磁盘/网络 I/O，Go 编译器拒绝循环。

## 兼容和所有权

保留全部 /api 路径、状态码、CSRF/限流/Cookie 属性、初始化并发冲突、下载/展示字段、计划预览和 dataText。任务切换类型必须清除另一类型的遗留字段；停用的过去一次性任务可以保存，但重新启用须按现有规则校验。Docker 目标仅允许现有名称/标签范围，不能从 task payload 任意选择容器 ID；编辑未改变目标且未重新启用时不增加无意义的 Docker 访问。

调度 engine 拥有执行锁、并发计数和关闭状态，store 拥有事务与 schema，HTTP 拥有请求和 session。关闭等待执行者退出再关闭数据库；恢复运行记录规则保持不变。单实例写一个数据库，任务数据和管理员密码散列均视为敏感资料。

仓储在离线恢复事务中调用 schedule.Next 这一纯领域规则，保证任务与过期运行记录原子恢复；它不启动调度或执行任务。domain 的请求头大小写使用 net/textproto.CanonicalMIMEHeaderKey 纯格式化函数，不构造 HTTP 请求或客户端。
