# WorkBuddy 连接器接入

JellyHub 网页登录（GitHub 或邮箱密码）与 WorkBuddy 连接器授权是两层能力。连接器不索要用户的 GitHub 凭据或 JellyHub 密码，也不把手机设备令牌交给 WorkBuddy。`/mcp` 同时接受控制台签发的静态 MCP 令牌和连接器 OAuth 访问令牌。连接器包位于 [`integrations/workbuddy/jellyhere/`](../integrations/workbuddy/jellyhere/)，其 URL 指向 JellyHub SaaS 实例；自托管者自行配置自己的服务地址。

## 从盈序门店连接器复用的经验

盈序门店连接器使用 `streamableHttp` MCP、`auth_mode: server-side`、`server_auth.auth_type: mcp_oauth`。包中有 `connector-meta.json`、`mcp.json`、图标和 Skill，验证脚本检查两处 MCP URL 一致、scope 精确匹配、版本一致、包内没有密钥。服务端做受保护资源与授权服务器元数据、动态客户端注册、PKCE S256、严格回调地址匹配、访问与刷新令牌、撤销，以及每次 MCP 调用的权限校验。JellyHere 复用了这些协议约束，并以设备为授权边界。

WorkBuddy 官方也允许静态令牌 `auth_mode: token`，但用户须手动从控制台生成并填写。当前公开连接器使用 OAuth，让用户在 JellyHub 登录并选择设备与权限。

## 已实现的授权流程

1. 未授权 `/mcp` 返回 `401` 与 `WWW-Authenticate`，指向 `/.well-known/oauth-protected-resource`。后者关联 `/.well-known/oauth-authorization-server`。
2. WorkBuddy 动态注册客户端后打开 `/oauth/authorize`。回调只接受官方列出的 HTTPS 域名，并与注册值完全一致。授权码绑定客户端、回调和 PKCE S256，5 分钟内一次性使用。
3. 用户以 GitHub 或邮箱密码登录 JellyHub，显式勾选设备。请求控制权限时，授权页默认只读；用户需另选“查看并操作手机”才获得控制。新设备不会自动加入已有连接。
4. 服务端只保存令牌哈希。访问令牌有效 1 小时，刷新令牌有效 30 天且轮换；控制台与 OAuth 撤销接口均可使令牌失效。删除设备时连带撤销关联连接。MCP 请求按租户、权限与设备 ID 过滤工具和数据。
5. 本地端到端测试覆盖 DCR、PKCE、默认只读、显式控制、设备范围、刷新、重放和撤销。WorkBuddy 平台中的真实安装与授权仍需验收。

连接器申请 `device.read device.control`，其中 `device.control` 必须在授权页单独选中才会生效。`device.read` 提供状态、界面与应用信息；控制权限提供点击、滑动、输入等开发测试操作。最终下单和支付不由普通控制授权代替。

## 打包与审核

在仓库根目录运行 `python3 scripts/validate-workbuddy-connector.py --package`，得到 `dist/workbuddy/jellyhere-connector-0.1.0.zip`。提交审核前在 WorkBuddy 开发者入口导入这个包，使用独立测试账户完成登录、设备选择、工具发现、只读与控制权限以及撤销测试。审核测试账户应使用单独空间和专用测试设备，不能指向维护者的个人手机。若平台对 OAuth 字段给出兼容性错误，再按其回调与服务日志调整。

参考：[WorkBuddy 连接器官方规范](https://open.workbuddy.cn/docs/connector)、[MCP Authorization 规范](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)。
