# WorkBuddy 连接器接入计划

JellyHub 网页登录（GitHub 或邮箱密码）与 WorkBuddy 连接器授权是两层能力。连接器不应索要用户的 GitHub 凭据或 JellyHub 密码，也不应把手机设备令牌交给 WorkBuddy。当前 `/mcp` 只接受 JellyHub 控制台签发的静态 MCP 客户端令牌；这适合开发者手动接入，不等于标准 MCP OAuth。

## 从盈序门店连接器复用的经验

现有盈序门店连接器使用 `streamableHttp` MCP、`auth_mode: server-side`、`server_auth.auth_type: mcp_oauth`。包中有 `connector-meta.json`、`mcp.json`、图标和 Skill，验证脚本检查两处 MCP URL 一致、scope 精确匹配、版本一致、包内没有密钥。服务端做了受保护资源与授权服务器元数据、动态客户端注册、PKCE S256、严格回调地址匹配、访问与刷新令牌、撤销，以及每次 MCP 调用的 scope 校验。JellyHere 应继承这些协议约束和打包检查，但重新定义设备授权模型，不能复制门店业务 scope 或固定回调路径。

WorkBuddy 官方文档允许静态令牌 `auth_mode: token`，但用户须手动从控制台生成并填入。面向公开 SaaS 的顺畅连接体验应采用 `auth_mode: server-side` 和 `mcp_oauth`，让用户在 JellyHub 登录后批准连接器。两种模式若同时发布，必须是不同 `source` 的独立连接器。

## JellyHere OAuth 设计

1. `GET /mcp` 未授权响应 `401` 与 `WWW-Authenticate`，指向 `/.well-known/oauth-protected-resource`；元数据关联授权服务器。
2. 授权服务器公布 `/.well-known/oauth-authorization-server`，支持动态客户端注册、`/oauth/authorize`、`/oauth/token`、刷新和撤销。客户端注册只接受受允许的 WorkBuddy 回调域名或经过逐项确认的本地回调；回调必须与注册值完全一致，授权码必须绑定客户端、回调、PKCE S256 和短有效期。
3. `/oauth/authorize` 使用已有 GitHub 或邮箱网页会话识别用户，显示“WorkBuddy 请求访问哪些设备、只读还是控制”的明确授权页。用户必须主动选设备并确认；未授权新设备不能自动加入既有连接器。操作能力与查看能力分开申请。
4. 令牌只保存哈希，访问令牌短有效期、刷新令牌可轮换并支持撤销；每次 MCP 请求检查租户、scope、设备 ID 与手机端暂停状态。设备删除或用户撤销连接器后，关联令牌立即失效。最终下单、支付仍需手机端单独确认。
5. 完成端到端 OAuth 测试、回调白名单测试、跨租户测试、令牌刷新/撤销测试、连接器包验证与 WorkBuddy 实机授权测试后再提交审核。

首版建议的权限是 `device.read`（状态、界面与应用信息）和 `device.control`（点击、滑动、输入等测试操作）。设备集合在授权页面选择，并在令牌服务端记录，不放在 OAuth scope 字符串里。先做只读连接器审核，控制权限经独立授权和用户可见的撤销入口再开放。

参考：[WorkBuddy 连接器官方规范](https://open.workbuddy.cn/docs/connector)、[MCP Authorization 规范](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)、[本项目托管试验说明](managed-pilot.md)。
