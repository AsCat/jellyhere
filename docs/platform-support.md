# JellyHere 平台支持规划

JellyHere 是连接个人设备与 AI 智能体的开源项目。**首个可用版本只支持 Android 设备端**；Go relay、MCP 和网页查看器不限定未来只能连接 Android。各平台的系统权限不同，设备端会报告自身能力，智能体只调用实际可用的工具。

| 平台 | 计划中的设备端 | 画面 | 界面元素与操作 | 状态 |
| --- | --- | --- | --- | --- |
| Android 11+ | 原生 Kotlin App | 按需截图 | 无障碍节点树、点击、滑动、输入等受控测试动作 | 已实现，首版重点 |
| iPhone / iPad | Swift App | 用户主动开启的系统屏幕共享期间取帧 | 普通第三方 App 无已确认的公开跨 App 元素树和全局手势接口；自有 App 可另接测试接口 | 只读 PoC，尚未实现 |
| macOS | 原生辅助进程 | 授权后按需抓取屏幕或窗口 | 经系统辅助功能授权后访问和操作可访问元素 | 规划中 |
| Windows | 原生辅助进程 | 授权后捕获屏幕或窗口 | UI Automation 访问和操作可访问控件 | 规划中 |
| Linux 桌面 | 会话内辅助进程 | 依桌面环境使用 ScreenCast portal | 依桌面环境使用 RemoteDesktop portal；元素树需另选 AT-SPI 等方案验证 | 规划中 |

## 统一架构

```text
Android / iOS / macOS / Windows / Linux 设备端
      └── 主动连接 JellyHere relay（WSS；跨网络可用）
                    ├── MCP / HTTPS：智能体按需调用
                    └── 网页查看器：用户临时授权后查看
```

后续设备握手增加 `platform`、`protocolVersion` 和 `capabilities`，如 `screen.capture`、`ui.tree`、`ui.tap`、`ui.textInput`。每项能力再报告授权状态和限制，例如 `requiresUserStart`、`captureActive`、`supportsBackground`。`device_status` 显示这些信息；调用不支持的能力时返回稳定的 `unsupported` 错误。现有 Android WSS 消息先保持兼容，待协议版本化和旧客户端回归测试完成后扩展。通用 MCP 工具可复用，但工具说明和结果必须反映平台能力。

设备 ID 在同一 relay 中保持唯一，并显示平台、机型或主机名，避免多设备误操作。每个设备端都应有本地暂停与授权撤销。订单、支付等最终动作不能沿用现有的开发测试授权。

## 实施顺序

1. **Android 首版**：完成固定签名发布、多机型稳定性、协议版本与能力声明；维持单进程 Go relay 的单用户自托管形态。
2. **iOS 只读 PoC**：验证 ScreenCaptureKit 系统选择器、用户启动、后台持续取帧和停止后立即失效；旧系统再评估 ReplayKit。自有 App 的测试接口与 Mac/XCUITest 设备实验室分别规划。不要把屏幕共享称为任意 App 远程控制。
3. **桌面 PoC**：先选一个平台完成“授权 → 取屏 → 读取元素 → 操作 → 回读”闭环，再抽取通用设备协议。macOS、Windows 和 Linux 各自实现系统适配层。
4. **多平台发布**：分别提供安装包、权限说明与兼容矩阵。Android 正式版本不等待其他平台完成。

## 官方依据

- [Apple ScreenCaptureKit](https://developer.apple.com/documentation/screencapturekit)：iOS/iPadOS/macOS 屏幕流需要用户授权；iOS 版本及后台表现仍须真机验证。
- [Apple XCUIAutomation](https://developer.apple.com/documentation/xcuiautomation)：开发测试框架，不是普通 App 可调用的常驻跨 App 控制权限。
- [Apple macOS ScreenCaptureKit 示例](https://developer.apple.com/documentation/screencapturekit/capturing-screen-content-in-macos)和 [AXUIElement](https://developer.apple.com/documentation/applicationservices/axuielement_h)：分别说明 macOS 画面捕获、辅助功能元素访问。
- [Microsoft Windows.Graphics.Capture](https://learn.microsoft.com/en-us/windows/uwp/audio-video-camera/screen-capture)和 [Windows UI Automation](https://learn.microsoft.com/en-us/windows/win32/winauto/uiauto-uiautomationoverview)：分别用于画面捕获和控件访问。
- [XDG RemoteDesktop portal](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.RemoteDesktop.html)：Linux 桌面会话的屏幕共享与输入控制需要用户授予，具体支持依赖 portal 后端。
