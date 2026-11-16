# 添加到桌面：设计与实施计划

沿用 z-lottery 安装交互：独立外部脚本与样式、相对 manifest 和 PNG 地址；manifest 省略 id，以 start_url 默认身份保留代理挂载区分。页面提供公共安装入口和可访问的安装指引对话框。原有业务 API 和资源路径不改，不承诺业务整体支持子目录。

有 beforeinstallprompt 时只在点击后调用原生安装；其他情况按电脑、Android、iOS/iPadOS、Mac Safari、内置浏览器和不安全地址展示指引。只有 appinstalled 或独立显示模式隐藏入口；accepted 不视为安装完成。不增加 Service Worker、缓存、依赖或业务请求。

1. 先添加可失败的 Go 静态资源/manifest/PNG/HTML检查与 Node VM 安装行为回归。
2. 增加 manifest、独立安装脚本/样式、公共入口及对话框，扩展显式 embed（如适用）并保证 manifest MIME。
3. 从原 favicon 导出 180/192/512 PNG（由主代理执行），更新 README 与质量入口。
4. 运行完整 go run ./tools/check；主代理用静态资源与假 API 浏览器 fixture 检查桌面/移动/登录界面，避免真实服务、任务或通知。

当前分支实施，不提交或推送；测试只使用内存请求、Node VM 或临时数据。

## 验收记录

`go run ./tools/check` 通过（格式、vet、Go测试、构建及14项Node安装测试）；只读交叉审查已完成。假登录/空任务API的静态页面在桌面、390px iPhone和320px安卓模拟均无脚本错误、横向溢出、manifest解析/安装资格错误；登录前后入口可用、手机入口/关闭按钮44px、关闭后恢复焦点。未启动真实调度器或任务。未执行真实设备安装、Linux race或实际容器运行。
