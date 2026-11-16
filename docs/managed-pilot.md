# 邀请制托管试验

此模式适合维护者邀请少量开发者试用同一个 Go relay。每个租户有独立的设备连接空间；设备令牌、账户管理令牌和 MCP 客户端令牌分开签发。**它还不是开放注册的 SaaS**：没有邮箱登录、找回账户、计费、跨进程高可用或消费交易授权。现有单用户自托管模式与个人实例不需要迁移。

## 启动独立实例

在单独的服务器目录中保存状态文件和操作员令牌，均只允许服务账户读取。下面是本地开发启动示例，端口仅监听本机；不要把操作员接口直接代理到公网。

```bash
umask 077
mkdir -p /var/lib/jellyhere-managed
openssl rand -hex 32 > /var/lib/jellyhere-managed/operator-token
export JELLYHERE_OPERATOR_TOKEN="$(cat /var/lib/jellyhere-managed/operator-token)"
export JELLYHERE_MANAGED_STATE=/var/lib/jellyhere-managed/state.json
export JELLYHERE_LISTEN=127.0.0.1:18094
cd server
go run ./cmd/jellyhere-server
```

正式试用部署可先按[自托管指南](self-host.md)构建二进制并安装到 `/opt/jellyhere/jellyhere-server`，随后单独配置 systemd 服务。下面命令在服务器执行，不会改动已有 `jellyhere.service`：

```bash
sudo install -d -m 700 /etc/jellyhere
OPERATOR_TOKEN=$(openssl rand -hex 32)
printf 'JELLYHERE_OPERATOR_TOKEN=%s\nJELLYHERE_MANAGED_STATE=/var/lib/jellyhere-managed/state.json\nJELLYHERE_LISTEN=127.0.0.1:18094\n' \
  "$OPERATOR_TOKEN" | sudo tee /etc/jellyhere/managed.env >/dev/null
sudo chmod 600 /etc/jellyhere/managed.env
unset OPERATOR_TOKEN
sudo install -m 644 deploy/jellyhere-managed.service /etc/systemd/system/jellyhere-managed.service
sudo systemctl daemon-reload
sudo systemctl enable --now jellyhere-managed.service
curl -fsS http://127.0.0.1:18094/health
```

systemd 会创建单独的可写状态目录；状态文件保持 `0600`。Go 进程用 JSON 文件保存账户和令牌哈希，采用临时文件加原子替换；只支持单进程写入。备份状态文件时按敏感配置处理。`GET /health` 和 `/healthz` 只返回 `{"ok":true}`。

公网 HTTPS/WSS 入口可用 `./deploy/render-nginx.sh managed.example.com managed` 生成独立站点配置，反向代理本机 `18094` 端口。获取证书和启用 Nginx 的命令参见[自托管指南的域名与证书步骤](self-host.md#2-配置域名和证书)，但请使用独立域名与独立配置文件。模板仅公开 `/health`、`/device`、`/mcp` 与租户管理路径 `/tenant`；**不公开** `/manage/tenants`。维护者通过服务器本机或 SSH 端口转发调用操作员接口。不要复用个人实例的域名、Nginx 站点和全局令牌。

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

   响应中的 `owner_token` 交给该用户。租户 ID 仅是可读标签；授权依赖随机令牌。

2. 用户用 `owner_token` 创建手机凭据和智能体凭据：

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

3. 用户可用 `GET /tenant` 查看自己的设备在线状态和已授权客户端。`DELETE /tenant/clients/{id}` 撤销单个智能体令牌；`DELETE /tenant/devices/{id}` 断开手机并撤销包含该设备的客户端令牌；`POST /tenant/owner-token/rotate` 更换账户管理令牌，旧令牌随即失效。撤销阻止后续请求，已经发送到手机的动作可能仍需等待结束。

当前限制：每个 MCP 客户端每分钟最多 60 个 HTTP 请求；单实例最多 100 个租户，每租户最多 8 台设备和 16 个 MCP 客户端。模式使用单个 Go 进程和本地状态文件；后续若需要公开注册、多人运营或多节点扩容，再引入正式账户登录、持久数据库、配额和运营流程。支付、最终下单仍需独立的手机端确认机制，不由 `controller` 令牌代替。

邀请制模式目前只开放 WSS 设备连接、MCP 和租户凭据管理；默认自托管模式中的网页查看器、临时 HTTPS 口令与 `/api/v1` 接口尚未接入租户模型。手机 App 中可继续使用悬浮角色暂停远程操作，但临时 HTTPS 口令在此模式下不能配对。`controller` MCP 客户端仍有 Android 测试控制能力，应只发给可信的开发智能体。
