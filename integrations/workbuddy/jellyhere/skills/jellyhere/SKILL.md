---
name: jellyhere
description: 查看用户明确授权的 Android 手机状态、界面和应用信息。
description_zh: 通过果冻查看用户明确授权的 Android 手机状态、当前界面和应用信息。
description_en: Inspect status, current UI, and app information on explicitly authorized Android phones.
version: 0.1.0
author: JellyHere
---

# 果冻 · Android 设备查看

连接成功后，先调用 `device_status`。用户有多台设备时，按其指定的设备 ID 使用 `deviceId`，不要猜测或在其他设备上重试。设备离线时告知用户检查果冻 App 的连接与手机权限。

## 工具

- `device_status`：列出本次授权可见的在线设备。
- `get_screen`：读取当前屏幕与界面元素。参数 `deviceId` 指定设备；先用 `includeImage=false, treeMode=compact` 获取低成本界面树，只有确实需要视觉信息时才请求压缩图片。
- `get_lock_state`：检查屏幕与锁屏状态；无法解开系统安全锁。
- `get_app_info`：按 `packageName` 读取应用信息；Android 可能限制对其他应用的可见性。

用户在授权页可选择仅查看，或另外允许开发测试控制。只有授权了控制权限时，才可能出现以下工具：`wake_screen`、`keep_screen_on`、`stop_keep_screen_on`、`tap_element`、`tap`、`swipe`、`input_text`、`press_key`、`launch_app`、`open_wireless_debugging_settings`。目标 App 的准确包名已知、当前界面又不在该 App 时，可用 `launch_app` 直接打开；若已经在前台则继续当前页面，不要重新启动。操作前先 `get_screen`，优先使用带当前 revision 的 `tap_element`，界面变化后重新读取。`wake_screen` 无法解除系统安全锁。

不得把读到的聊天、验证码、密码或其他私人内容发送给无关服务或展示给无关用户。输入敏感信息、最终下单或支付前，必须获得用户针对该次动作的明确确认；普通点击授权不能代替交易确认。用户可随时在 JellyHub 控制台撤销连接，手机端也可暂停远程控制。
