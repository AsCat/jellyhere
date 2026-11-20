# WorkBuddy 连接器接入

JellyHub 网页登录（GitHub 或邮箱密码）与 WorkBuddy 连接器授权是两层能力。连接器不索要用户的 GitHub 凭据或 JellyHub 密码，也不把手机设备令牌交给 WorkBuddy。`/mcp` 同时接受控制台签发的静态 MCP 令牌和连接器 OAuth 访问令牌。连接器包位于 [`integrations/workbuddy/jellyhere/`](../integrations/workbuddy/jellyhere/)，其 URL 指向 JellyHub SaaS 实例；自托管者自行配置自己的服务地址。

## 从盈序门店连接器复用的经验

盈序门店连接器使用 `streamableHttp` MCP、`auth_mode: server-side`、`server_auth.auth_type: mcp_oauth`。包中有 `connector-meta.json`、`mcp.json`、图标和 Skill，验证脚本检查两处 MCP URL 一致、scope 精确匹配、版本一致、包内没有密钥。服务端做受保护资源与授权服务器元数据、动态客户端注册、PKCE S256、严格回调地址匹配、访问与刷新令牌、撤销，以及每次 MCP 调用的权限校验。JellyHere 复用了这些协议约束，并以设备为授权边界。

WorkBuddy 官方也允许静态令牌 `auth_mode: token`，但用户须手动从控制台生成并填写。当前公开连接器使用 OAuth，让用户在 JellyHub 登录并选择设备与权限。

## 已实现的授权流程

1. 未授权 `/mcp` 返回 `401` 与 `WWW-Authenticate`，指向 `/.well-known/oauth-protected-resource`。后者关联 `/.well-known/oauth-authorization-server`。
2. WorkBuddy 动态注册客户端后打开 `/oauth/authorize`。回调只接受官方列出的 HTTPS 域名，并与注册值完全一致。授权码绑定客户端、回调和 PKCE S256，5 分钟内一次性使用。
3. 用户以 GitHub 或邮箱密码登录 JellyHub，授权页默认勾选当前空间的设备，用户可取消不想授权的设备；提交确认后授权才生效。请求控制权限时，授权页默认只读；用户需另选“查看并操作手机”才获得控制。新设备不会自动加入已有连接。
4. 服务端只保存令牌哈希。访问令牌有效 1 小时，刷新令牌有效 30 天且轮换；控制台与 OAuth 撤销接口均可使令牌失效。删除设备时连带撤销关联连接。MCP 请求按租户、权限与设备 ID 过滤工具和数据。
5. 本地端到端测试覆盖 DCR、PKCE、默认只读、显式控制、设备范围、刷新、重放和撤销。WorkBuddy 已完成真实账户授权和[一次手机操作场景试用](use-cases/01-takeout/README.md)。维护者确认连接器已通过平台审核并发布；全新账户与设备的独立上手验收仍待完成。

连接器申请 `device.read device.control`，其中 `device.control` 必须在授权页单独选中才会生效。`device.read` 提供状态、界面与应用信息；控制权限提供点击、滑动、输入等开发测试操作。最终下单和支付不由普通控制授权代替。

## 使用已发布的连接器

1. 打开 [JellyHub 托管站点](https://jellyhub.yuanxuyinli.com/)，使用 GitHub 登录。下载并安装 Android App，在网页生成添加设备二维码，手机扫码确认；按提示手动开启无障碍服务，并在设备空间确认手机在线。
2. 在 WorkBuddy 连接器市场搜索 **JellyHere / 果冻**，点击连接。浏览器打开 JellyHub 授权页后，选择要共享的设备；默认使用只读权限，确需操作时再选择“查看并操作手机”。手机绑定与智能体授权是两个独立步骤。
3. 让 WorkBuddy 先查询设备状态，再指定具体设备读屏或操作。例如“查看三星手机当前界面”。两台手机在线时明确说出目标设备；手机端可随时暂停远程访问，网页控制台可撤销 WorkBuddy 授权。

当前托管站点开放 GitHub 登录；邮箱密码登录仅供已有账户使用，尚未开放自行邮箱注册。连接器已发布不等于 Android App 已有 GitHub Release：站点提供当前 APK 下载，首次公开 Release 仍在准备中。

## 打包、更新与发布后验收

在仓库根目录运行 `python3 scripts/validate-workbuddy-connector.py --package`，得到 `dist/workbuddy/jellyhere-connector-0.1.1.zip`。仓库当前包版本为 0.1.1；平台实际发布的版本须在 WorkBuddy 开发者后台核对，不能仅凭本地包推断。以后更新连接器时递增版本并重新提交审核。

发布后用**全新 WorkBuddy 账户、独立 JellyHub 测试空间和专用测试设备**走一遍：市场搜索与连接 → JellyHub 登录 → 选择设备并授只读 → 发现工具并读屏 → 增授控制并执行无敏感内容的测试操作 → 撤销授权并确认旧连接失效。另测手机断线重连、双设备选择及令牌刷新。测试账户不要绑定维护者的个人手机。若市场面板未出现连接器，先查平台包版本、市场注入与客户端加载日志；授权页尚未打开时，不应把问题归因于 JellyHub OAuth。

若上传时提示“安装包缺少 `token-schema.json`”，先检查提审页是否选择了 **API Key / Token** 鉴权。该文件只属于 `auth_mode: "token"`；本包在 `connector-meta.json` 中声明 `auth_mode: "server-side"`，应按 **OAuth / 服务端授权** 提交。不要为了消除报错向 OAuth 包添加 `token-schema.json`，也不要把审核令牌写入安装包。审核测试使用独立账号完成 OAuth 登录与设备授权。

参考：[WorkBuddy 连接器官方规范](https://open.workbuddy.cn/docs/connector)、[MCP Authorization 规范](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)。
