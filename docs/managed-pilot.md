# JellyHub 托管试验

此模式适合少量开发者共用一个 Go relay。每个账户有独立设备空间；设备令牌和 MCP 客户端令牌分开签发。用户可通过 GitHub 登录；配置 SMTP 后也可用邮箱密码注册，验证邮箱后创建空间。GitHub 用户可在原空间绑定邮箱密码，之后任选一种方式登录。未配置时仍可用邀请管理令牌。当前单实例上限 100 个账户，属于托管试验，尚无计费、多节点高可用或消费交易授权。

## 用户操作

1. 打开托管站点，使用 GitHub 登录；若站点配置了 SMTP，也可输入邮箱和密码注册，打开验证邮件后进入自己的设备空间。已有 GitHub 空间可在登录后绑定邮箱密码，避免生成第二个空间。
2. 在“我的设备”点击“添加设备”，下载正式签名的 Android APK，安装并打开果冻。
3. 网页点击“生成添加设备二维码”；手机 App 点击“扫码连接”，核对服务器和设备空间，再确认绑定。二维码 5 分钟有效、只能兑换一次；票据不包含可长期使用的设备令牌。手机自动保存设备连接配置并发起 WSS 连接。连接在其他服务器上的手机不会自动出现在当前空间；扫码切换后，原服务器将显示它离线。
4. 首次使用需由用户在 Android 系统设置里手动启用果冻无障碍服务。可按需允许通知、后台运行。
5. 回到网页，在设备卡片确认设备在线。Codex 等通用智能体可创建单独的只读或测试控制 MCP 令牌；WorkBuddy 连接器则在登录后通过浏览器授权并显式选择设备与权限。绑定手机本身不会授权任何智能体。管理页可撤销 WorkBuddy 连接，静态 MCP 客户端也可编辑设备范围。托管模式的手机悬浮菜单可打开当前服务器的移动版控制台。

扫码时请求相机权限。旧版 Android App 仍可手动填写服务器地址、设备 ID 和令牌。登录会话和未兑换的二维码票据目前保存在 Go 进程内；服务重启后需重新登录或重新生成二维码，已绑定设备凭据不会丢失。账户、设备及令牌哈希保存在单进程 JSON 状态文件中；扩大开放注册规模前应迁到数据库。

## 配置 GitHub 登录

在 GitHub OAuth App 设置中填写 Homepage URL `https://managed.example.com/`，Authorization callback URL `https://managed.example.com/auth/github/callback`。OAuth App 只需读取 GitHub 基本身份，不申请额外 scope。将 Client ID 和 Client secret 写入仅服务账户可读的 `/etc/jellyhere/managed.env`：

```text
JELLYHERE_PUBLIC_URL=https://managed.example.com
JELLYHERE_GITHUB_CLIENT_ID=<client id>
JELLYHERE_GITHUB_CLIENT_SECRET=<client secret>
```

重启 `jellyhere-managed.service` 后，`GET /auth/config` 的 `github_enabled` 应为 `true`，首页才显示 GitHub 登录按钮。不要把 secret 放入 Git、网页或 APK。公网 Nginx 应转发 `/auth/`、`/pair/`、`/tenant/`、`/.well-known/oauth-*` 和 `/oauth/`，并提供固定签名的 `/download/jellyhere.apk`；`/manage/tenants` 仍只在本机访问。OAuth state、会话 Cookie 和网页写操作的 Origin 均由服务端校验。

## 配置邮箱注册与登录

邮箱注册默认关闭，邮箱密码登录入口保留。未配置 SMTP 时，维护者可以在**仅本机开放**的操作员接口创建独立测试账户，供连接器审核使用；不要把自己的手机设备空间交给审核账号。配置 SMTP 后，`GET /auth/config` 的 `email_registration_enabled` 变为 `true`，用户可以注册或在已有 GitHub 空间中绑定邮箱。验证链接 15 分钟有效，验证完成前不会创建账户；密码使用 Argon2id 加独立盐存储。登录尝试有限流。邮箱密码是 JellyHub 的网页登录方式，不会直接充当 MCP 令牌或 WorkBuddy 的 OAuth 凭据。当前尚未提供忘记密码自助重置，正式扩大开放前必须补齐。

操作员在服务器本机可调用 `POST /manage/email-accounts`，提交 `{"email":"reviewer@example.com","password":"<至少 12 位随机密码>"}` 并携带操作员 Bearer 令牌。不传 `tenant_id` 时会创建全新空设备空间；传已有 `tenant_id` 时只为该空间增加一种登录方式。响应不回显密码。此接口不可经 Nginx 公开，测试密码须从凭据管理器单独交付，不要写入仓库或聊天记录。

发信服务可使用支持 STARTTLS 的 SMTP 服务。阿里云邮件推送的接入步骤是：开通服务，创建独立发信子域名并按控制台要求配置 DNS 记录，验证域名，创建发信地址并单独设置 SMTP 密码。阿里云杭州区的 SMTP 地址为 `smtpdm.aliyun.com`，可用端口 `80` 的 STARTTLS；配置时不要用 ECS 默认受限的 25 端口。把下列环境变量放入仅服务账户可读的 `/etc/jellyhere/managed.env`，重启服务，再用自己可接收邮件的邮箱做一次注册验收：

```text
JELLYHERE_SMTP_HOST=smtpdm.aliyun.com
JELLYHERE_SMTP_PORT=80
JELLYHERE_SMTP_USER=hello@mail.example.com
JELLYHERE_SMTP_PASSWORD=<SMTP 专用密码>
JELLYHERE_SMTP_FROM=hello@mail.example.com
```

`SMTP_USER` 与 `SMTP_FROM` 应使用同一个已验证发信地址。后端要求 STARTTLS；服务端不回显 SMTP 凭据。SMTP 未配置时，不在首页展示注册表单，但已有邮箱账户仍可登录。

## 启动独立实例

在单独的服务器目录中保存状态文件和操作员令牌，均只允许服务账户读取。下面是本地开发启动示例，端口仅监听本机；不要把操作员接口直接代理到公网。

```bash
umask 077
mkdir -p /var/lib/jellyhere-managed
openssl rand -hex 32 > /var/lib/jellyhere-managed/operator-token
export JELLYHERE_OPERATOR_TOKEN="$(cat /var/lib/jellyhere-managed/operator-token)"
export JELLYHERE_MANAGED_STATE=/var/lib/jellyhere-managed/state.json
export JELLYHERE_LISTEN=127.0.0.1:18094
export JELLYHERE_PUBLIC_URL=https://managed.example.com
cd server
go run ./cmd/jellyhere-server
```

正式试用部署可先按[自托管指南](self-host.md)构建二进制并安装到 `/opt/jellyhere/jellyhere-server`，随后单独配置 systemd 服务。下面命令在服务器执行，不会改动已有 `jellyhere.service`：

```bash
sudo install -d -m 700 /etc/jellyhere
OPERATOR_TOKEN=$(openssl rand -hex 32)
printf 'JELLYHERE_OPERATOR_TOKEN=%s\nJELLYHERE_MANAGED_STATE=/var/lib/jellyhere-managed/state.json\nJELLYHERE_LISTEN=127.0.0.1:18094\nJELLYHERE_PUBLIC_URL=https://managed.example.com\n' \
  "$OPERATOR_TOKEN" | sudo tee /etc/jellyhere/managed.env >/dev/null
sudo chmod 600 /etc/jellyhere/managed.env
unset OPERATOR_TOKEN
sudo install -m 644 deploy/jellyhere-managed.service /etc/systemd/system/jellyhere-managed.service
sudo systemctl daemon-reload
sudo systemctl enable --now jellyhere-managed.service
curl -fsS http://127.0.0.1:18094/health
```

systemd 会创建单独的可写状态目录；状态文件保持 `0600`。Go 进程用 JSON 文件保存账户和令牌哈希，采用临时文件加原子替换；只支持单进程写入。备份状态文件时按敏感配置处理。`GET /health` 和 `/healthz` 只返回 `{"ok":true}`。

公网 HTTPS/WSS 入口可用 `./deploy/render-nginx.sh managed.example.com managed` 生成独立站点配置，反向代理本机 `18094` 端口。获取证书和启用 Nginx 的命令参见[自托管指南的域名与证书步骤](self-host.md#2-配置域名和证书)，但请使用独立域名与独立配置文件。模板公开首页、登录、扫码绑定、APK 下载、`/health`、`/device`、`/mcp` 与租户管理路径 `/tenant`；**不公开** `/manage/tenants`。维护者通过服务器本机或 SSH 端口转发调用操作员接口。不要复用个人实例的域名、Nginx 站点和全局令牌。

## 邀请与接入

以下示例中的变量只在当前终端保存。响应会显示新令牌一次；记录到自己的凭据管理器，勿提交或贴到 Issue。

1. 维护者在服务器本机或 SSH 隧道中创建租户：

   ```bash
   OPERATOR_TOKEN=$(sudo sed -n 's/^JELLYHERE_OPERATOR_TOKEN=//p' /etc/jellyhere/managed.env)
   curl -sS -X POST http://127.0.0.1:18094/manage/tenants \
     -H "Authorization: Bearer $OPERATOR_TOKEN" \
     -H 'Content-Type: application/json' -d '{"tenant_id":"alice"}'
   unset OPERATOR_TOKEN
   ```

   响应中的 `owner_token` 交给该用户，或发一次邀请链接 `https://managed.example.com/#owner=<owner_token>`。令牌放在 URL 片段中，页面读取后会立即从地址栏移除，并只保存在当前浏览器会话中。用户也可以在首页手动粘贴令牌。租户 ID 仅是可读标签；授权依赖随机令牌。当前没有账户找回流程，用户须自行保存管理令牌。

2. 用户在首页用表单创建手机凭据和智能体凭据；也可用 `owner_token` 调用 API：

   ```bash
   curl -sS -X POST https://managed.example.com/tenant/devices \
     -H "Authorization: Bearer $OWNER_TOKEN" \
     -H 'Content-Type: application/json' -d '{"device_id":"primary"}'

   curl -sS -X POST https://managed.example.com/tenant/clients \
     -H "Authorization: Bearer $OWNER_TOKEN" \
     -H 'Content-Type: application/json' \
     -d '{"client_id":"codex","scope":"viewer","device_ids":["primary"]}'
   ```

   手机果冻 App 的服务器地址填写 `wss://managed.example.com/device`，设备 ID 填 `primary`，设备令牌填第一条响应的 `device_token`。App 保存时会自行在 URL 附加 `?id=primary`。Codex、WorkBuddy 或 `jellyhere` CLI 填写 `https://managed.example.com/mcp` 与第二条响应的 `mcp_token`。手机与电脑仍只发起出站连接，不要求同一局域网。`viewer` 只能查看状态、屏幕与应用信息；确需开发控制时另建 `controller` 客户端。客户端凭据始终限定明确列出的设备 ID。

3. 用户可用 `GET /tenant` 查看自己的设备在线状态和已授权客户端。`PATCH /tenant/clients/{id}` 携带 `{"device_ids":["primary","samsung"]}` 可调整该客户端的设备范围，原令牌和只读/控制权限保持不变；`DELETE /tenant/clients/{id}` 撤销单个智能体令牌；`DELETE /tenant/devices/{id}` 断开手机并撤销包含该设备的客户端令牌；`POST /tenant/owner-token/rotate` 更换账户管理令牌，旧令牌随即失效。撤销阻止后续请求，已经发送到手机的动作可能仍需等待结束。

当前限制：每个 MCP 客户端每分钟最多 60 个 HTTP 请求；单实例最多 100 个租户，每租户最多 8 台设备和 16 个 MCP 客户端。模式使用单个 Go 进程和本地状态文件；规模扩大后需引入数据库、持久会话、配额和运营流程。支付、最终下单仍需独立的手机端确认机制，不由 `controller` 令牌代替。

服务端在 systemd journal 中记录 `mcp_request` 和 `device_call` 元数据，并用 `trace` 关联一次 MCP 请求与手机往返。字段包括工具方法、耗时、排队时间、结果字节数和成功/失败类别；不记录工具参数、界面树、截图或输入文字。可用 `journalctl -u jellyhere-managed.service` 查看。它用于分析调用频率与设备响应耗时，不包含智能体内部思考或第三方 App 加载时间的完整分解。[短时完整诊断录制的设计](observability.md)另行说明。

托管模式目前开放 WSS 设备连接、MCP、账户登录、扫码绑定和租户凭据管理；默认自托管模式中的网页查看器、临时 HTTPS 口令与 `/api/v1` 接口尚未接入租户模型。手机 App 中可继续使用悬浮角色暂停远程操作，但临时 HTTPS 口令在此模式下不能配对。`controller` MCP 客户端仍有 Android 测试控制能力，应只发给可信的开发智能体。
