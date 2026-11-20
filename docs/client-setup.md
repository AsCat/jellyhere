# MCP 客户端接入

下面的手动 MCP 配置面向[自托管](self-host.md)实例：把 `your-domain.example` 换成自己的域名。也可使用可选的 [JellyHub 托管试验](managed-pilot.md)；其 WorkBuddy 连接器通过浏览器 OAuth 授权，无需手动填写 MCP 令牌。手动配置的客户端需支持 MCP Streamable HTTP，并在每个请求中发送 `Authorization: Bearer <MCP 令牌>`。手机使用的是另一枚设备令牌，不能混用。

## Codex CLI / 桌面端

在启动 Codex 的环境里设置 `JELLYHERE_MCP_TOKEN`，配置 `~/.codex/config.toml`（若设置了 `CODEX_HOME`，则改为该目录下的 `config.toml`）：

```toml
[mcp_servers.jellyhere]
url = "https://your-domain.example/mcp"
bearer_token_env_var = "JELLYHERE_MCP_TOKEN"
default_tools_approval_mode = "writes"
tool_timeout_sec = 30
```

`writes` 模式会对点击、滑动、输入和按键操作提示审批；`device_status`、`get_screen` 和 `get_app_info` 已标记只读。配置后重启 Codex，运行 `codex mcp get jellyhere` 核对配置，或在界面中用 `/mcp` 查看。旧配置中的 `mcp_servers.orbbuddy` 仍可连接同一 URL；名称只是本地别名。桌面端若无法继承 shell 环境变量，可按 [Codex 官方 MCP 文档](https://learn.chatgpt.com/docs/extend/mcp?surface=cli) 使用 `http_headers_helper` 从本机凭据存储读取令牌。

## 其他智能体

WorkBuddy 使用已发布的 [JellyHere 连接器](workbuddy-connector-plan.md#使用已发布的连接器)即可通过 JellyHub OAuth 连接；其他支持远程 Streamable HTTP MCP 的客户端可填写自托管 URL 和 Bearer 令牌。若客户端只支持 stdio，需要一个本地 stdio 转发适配器；该适配器尚未纳入 MVP。不能仅凭产品名称推断其 MCP 兼容性。

先调用 `device_status`，确认目标设备在线且 `paused` 为 `false`。多台手机在线时，所有读屏和操作工具都要显式传 `deviceId`，例如 `primary` 或 `samsung`；省略时会返回歧义错误。手机端暂停时，所有读屏和操作命令会被拒绝，用户需在手机 App 或悬浮球上恢复。界面树有 `path` 和 `revision`，元素操作优先使用 `tap_element`；修订号过期时重新读屏。只判断文字或控件状态时用 `get_screen` 的 `includeImage=false` 和 `treeMode="compact"`；精简结果返回 `elements` 平铺列表，每项的 `path` 仍可传给 `tap_element`，`bounds` 为原始像素 `[left,top,right,bottom]`。需要视觉判断时才取图片，可设 `maxImageEdge=640` 等较小尺寸。默认仍是完整界面树和最长边 1280 像素、JPEG 质量 70 的图片。若目标 App 的包名已知且当前不在该 App，可用控制工具 `launch_app` 直接打开，再重新读屏；包名不确定时不要猜测。

屏幕没有节点时先调用 `get_lock_state`。若 `interactive=false`，可显式调用 `wake_screen`，再查看返回的 `interactive` 与 `deviceLocked`；设备需要系统认证时请让用户在手机上解锁，再重试读屏。`wake_screen` 只请求唤亮，不输入 PIN，也不保证各厂商系统都会响应。它属于操作工具；`get_lock_state` 是只读工具。

在已解锁且屏幕已亮时，可调用 `keep_screen_on`，例如 `{"deviceId":"samsung","minutes":10}`，避免测试中自动熄屏和随后的锁定。最长 30 分钟，到期自动结束；`get_lock_state` 返回 `keepAwakeRemainingMs`。调用 `stop_keep_screen_on`、手机通知的“结束亮屏”、暂停果冻或断开会话均可结束。此操作不修改系统的全局屏幕超时设置；按电源键手动锁屏仍有效。显示屏 wake lock 是旧版 Android API，需按设备验证。

## 无线调试与果冻的关系

果冻运行时不依赖无线 ADB：手机用移动网络也可主动连接 relay。无线 ADB 只用于开发时安装、调试 APK。`open_wireless_debugging_settings` 尝试打开系统设置；是否已开启须继续读屏确认。电脑端安装 `jellyhere` 后，可用 `jellyhere doctor` 和 `jellyhere device list` 检查连接；`jellyhere adb connect --device <id>` 按需恢复无线调试，旧 `orb-adb` 仍可用。智能体入口约定见仓库根目录的 [AGENTS.md](../AGENTS.md)，完整流程见[快捷操作与多设备](shortcuts.md)。果冻离线时电脑端命令无法靠它打开设置。

普通第三方 App 不能直接写入无线调试相关的安全设置；Android 将 `WRITE_SECURE_SETTINGS` 标为[不供第三方应用使用](https://developer.android.com/reference/android/Manifest.permission#WRITE_SECURE_SETTINGS)。[Android 官方无线 ADB 流程](https://developer.android.com/tools/adb#connect-to-a-device-over-wi-fi)要求先在手机上启用无线调试、完成配对，并让工作站与设备处于同一 Wi-Fi 网络。跨网使用还需要另行解决两端的网络可达性；果冻的 HTTPS/WSS 中转只转发果冻命令，不转发 ADB 连接。
