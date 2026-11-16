# HTTPS API：审计、接入与验收

补充需求文档使用了“Gill”名称；本机及当前会话可确认的既有实现是 **JellyHere（果冻）**，没有发现独立 Gill Skill 或代码仓库。本次在果冻上增量实现，原 MCP 工具名、设备 WSS 命令格式和设备令牌保持不变。

## 现状审计与复用

| 项目 | 既有实现 | 本次处理 |
| --- | --- | --- |
| 手机端 | 独立 Android App；无障碍服务保持 WSS 出站连接，前台服务保活 | 增本地口令和撤销 UI |
| 截图 | get_screen 按需调用 Android takeScreenshot，返回 JPEG Base64、界面树和原始屏幕尺寸 | 复用；加采集时间，HTTPS 只返回 JPEG |
| 操作 | tap、swipe、press_key、tap_element、input_text 由无障碍服务执行 | HTTPS 只映射 tap/swipe/back/home；MCP 不变 |
| 中转 | Go Hub 管理设备 WebSocket、心跳、20 秒调用超时和串行操作 | 复用；增设备撤销事件和离线元数据 |
| 对外访问 | Nginx HTTPS/WSS，Go 本地监听，独立 MCP 与设备令牌 | 增 HTTPS API、单次分享、首页及 OpenAPI |

## 手机授权

1. 果冻连上服务器后，在 App 点“生成只读口令（60 秒）”，或填写已安装的可调试 App 包名并点“生成开发测试控制口令”。
2. 手机显示随机 4 位码。客户端 60 秒内调用 POST /api/v1/auth/pair；码使用一次即清除。连续错误 3 次冷却 5 分钟，手机显示提示。
3. 服务端签发高熵 Bearer 令牌。状态查询、截图元数据和操作回执查询不续期；截图、操作、生成分享链接会续空闲时间。空闲 5 分钟或总计 30 分钟失效。
4. 手机 App 的“立即撤销 HTTPS 授权”清除设备端授权并通知服务器删除会话。客户端也可 POST /api/v1/auth/revoke。

手机端仅同时保留一个 HTTPS 授权；再次成功配对会替换旧授权。设备断线后状态可显示离线，重连会使旧令牌失效，需重新配对。Go 进程重启也会清除全部临时会话。

## 调用

自托管实例首页 `https://your-domain.example/` 含网页查看器：手机生成只读口令后，在首页填写设备 ID（默认 `primary`）和 4 位口令，即可查看状态、按需刷新截图并撤销授权。网页不提供点击、滑动或输入；即使用控制口令配对，网页仍只读。令牌只放在当前页面内存中；关闭页面只清除本地令牌，如需立即撤销服务端会话，请点击网页“撤销授权”或在手机端撤销。配对后获取首张截图，此后只在用户点击刷新时采集。机器可读契约位于同一域名的 `/openapi.json`。下例中的域名、口令与令牌均需换成自己的值：

公开存活检查为 `GET /health`，无需授权，固定返回 `{"ok":true}`。Nginx 将它精确转发到本机 `/healthz`；响应不包含设备状态、截图或令牌，也不缓存。此接口只表示 Web 入口和 Go 进程可用，不代表手机在线。

    curl -sS -X POST https://your-domain.example/api/v1/auth/pair \
      -H 'Content-Type: application/json' \
      -d '{"device_id":"primary","code":"0000"}'

    TOKEN='粘贴 access_token'
    curl -H "Authorization: Bearer $TOKEN" \
      https://your-domain.example/api/v1/devices/primary/status
    curl -H "Authorization: Bearer $TOKEN" \
      https://your-domain.example/api/v1/devices/primary/screenshot -o screen.jpg

截图总是向手机请求新画面；max_age_ms 可传 0–60000，但当前版本不读取缓存。成功响应是 image/jpeg，含 Cache-Control: no-store、X-Captured-At、X-Request-ID 和原始屏幕尺寸头。元数据接口只保留时间、分辨率和修订号；断线或重连后的旧图不标为新鲜。手机拒绝截图时返回错误，不返回历史图片。

控制会话在指定可调试 App 位于前台时可以发送：

    curl -X POST https://your-domain.example/api/v1/devices/primary/actions \
      -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
      -d '{"request_id":"tap-001","action":"tap","params":{"x":320,"y":600}}'

swipe 参数是 x1,y1,x2,y2,duration_ms，时长 50–5000 毫秒；back/home 无参数。同一会话内相同 request_id 与动作只执行一次，动作内容不同则返回 409。响应 status 为 executed 或 failed；GET /api/v1/operations/{requestId} 可查询回执。Go Hub 串行执行同一设备的命令，坐标由手机端按当前原始屏幕尺寸最终校验。

执行 back 或 home 后，HTTPS 控制立即暂停，令牌仍可读屏和查询回执；若要继续控制，须重新在手机生成控制口令并配对。这避免界面切换期间的后续手势落到其他 App。

只会打开 URL 的客户端，可由已授权客户端先 POST /api/v1/shares，取返回的 share_url。该链接仅 60 秒有效、只能读取一张最新截图，第一次请求后即失效；服务端不预存图片。分享链接本身是临时访问凭据，避免转发给无关方。Nginx 的 /share/ 路径关闭访问日志。

## 边界与错误

- viewer 只读；controller 必须由手机端明确选择，且只能操作指定的可调试 App。HTTPS API 没有输入文字、任意系统命令或交易确认接口。原 MCP 是独立授权路径，仍按原有开发测试约束使用。
- 手机锁屏、受保护窗口或目标 App 禁止截屏时，Android 可能返回截图错误或空白/受保护内容；接口不会伪造旧图。screen_capture_available 是连接状态提示，不保证目标窗口可截。
- 常见状态码：401 未授权/过期；403 权限不符或手机撤销；404 设备未知；409 设备拒绝/截图不可用/请求 ID 冲突；410 分享链接失效；429 冷却或限流；503 手机离线/暂停；504 超时。
- Go 不记录口令、会话令牌、截图或输入文本。配对成功/失败、请求 ID、操作类型和结果进入服务日志。每个会话最多 60 次请求/分钟、5 条分享链接和 200 个操作回执；临时会话只存内存。
- 普通 ChatGPT 网页抓取可能不能提交 POST 和 Bearer 头，也未必会执行首页 JavaScript。网页查看器面向可交互的浏览器；能读取首页不代表能直接调用受控 API。一次性分享链接可单独验证 URL 读图。智能体完整操作需要支持认证 HTTPS 请求的连接器，或继续使用现有 MCP 接口。

## 验收与回滚

自动测试覆盖原 MCP 截图/点击链路、手机 WebSocket 模拟、一次性配对、只读权限、截图不缓存、操作幂等、错误 3 次冷却、空闲/绝对过期、跨设备拒绝、一次性分享和撤销。运行 cd server && go test -race ./...；Android 运行 ANDROID_HOME=/path/to/sdk ./gradlew :app:assembleDebug。

线上部署前备份 Go 可执行文件与 Nginx 配置，运行 `nginx -t`，再重启 Go 和重载 Nginx。新安装的二进制位于 `/opt/jellyhere/jellyhere-server`，站点配置由部署者自行指定；回滚时恢复各自的备份，并执行 `nginx -t && systemctl reload nginx && systemctl restart jellyhere.service`。

发布前至少在两种 Android 机型上验证：只读口令配对、重复口令拒绝、状态与截图、只读操作拒绝、一次性分享链接、手机撤销、控制会话的指定 App 限制和重复 `request_id`。记录版本、网络环境、操作结果和脱敏日志；实例域名与内部路径留在维护者私有验收记录中。
