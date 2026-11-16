# 从 OrbBuddy 迁移到 JellyHere

项目名称、GitHub 仓库地址、Android 桌面显示名称、Go 模块和新安装的服务文件改为 JellyHere（中文“果冻”）。现有连接协议和权限边界保持不变。

## 已有 Android 安装

Android `applicationId` 和 Kotlin 包名仍为 `dev.orbbuddy.android`，发布签名别名仍为 `orbbuddy-release`；这些是安装兼容标识。使用同一发布密钥构建的较高 `versionCode` APK 可以覆盖旧正式包，原有设备 ID、服务器地址和令牌继续存放在应用私有数据里。调试包与正式包签名不同，不能相互覆盖。不要为了品牌统一而修改包名或重建发布密钥。

## 已有服务器

已有安装可继续使用旧的 `/opt/orbbuddy`、`/etc/orbbuddy/server.env`、`orbbuddy.service` 和原域名。新 Go 二进制继续接受 `ORBBUDDY_DEVICE_TOKEN`、`ORBBUDDY_MCP_TOKEN`、`ORBBUDDY_LISTEN`；新的 `JELLYHERE_*` 名称优先。已有服务只需把新构建的二进制安装到旧 `ExecStart` 指定的位置，按原升级与回滚步骤重启。更改系统目录或域名是单独的运维迁移，不是这次改名的前提。新用户按[自托管指南](self-host.md)使用 `jellyhere.service` 和 `/etc/jellyhere`。

## MCP 与电脑快捷工具

MCP URL 和工具名不变。客户端的 `[mcp_servers.orbbuddy]` 是本地别名，仍可继续用；新配置示例用 `[mcp_servers.jellyhere]`。旧 `ORBBUDDY_MCP_TOKEN` 可以照常用作客户端读取令牌的本地变量，新示例改用 `JELLYHERE_MCP_TOKEN`。电脑上的 `orb-adb` 命令和 `tools/orb_adb_connect.py` 暂时保留，避免打断现有一键恢复流程；新脚本也识别 `JELLYHERE_*` 环境变量与 `~/.config/jellyhere/`，并回退读取旧配置。

## 发布签名

维护者的 keystore 物理路径和 macOS 钥匙串条目仍沿用旧名称，不应移动或重建。构建脚本支持新的 `JELLYHERE_RELEASE_KEYSTORE`、`JELLYHERE_RELEASE_STORE_PASSWORD`，同时继续接受旧变量。详见[发布签名说明](release-signing.md)。
