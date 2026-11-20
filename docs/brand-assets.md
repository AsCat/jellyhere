# 果冻品牌图标

确认的主图标是暖黄色“星星果冻”配清爽蓝 `#3B6FAB`。矢量母版位于 [`assets/brand/jellyhere-icon.svg`](../assets/brand/jellyhere-icon.svg)；Android、网站和 WorkBuddy 的静态图标均由此生成。手机悬浮角色沿用现有的可动画星星实现，新安装用户默认选择它；已保存其他角色偏好的用户不会被覆盖。

在 macOS 安装 `librsvg`，或在 Linux 安装 `librsvg2-bin` 后，从仓库根目录运行：

```bash
python3 scripts/sync-brand-assets.py
python3 scripts/sync-brand-assets.py --check
```

脚本同步 WorkBuddy `icon.svg`、Go 服务内嵌的 `/brand-icon.svg`，并生成 Android adaptive icon 前景图和各密度的桌面图标。Android 通知栏使用单色星形轮廓，这是系统状态栏对小图标的显示形式。若调整配色或造型，先编辑矢量母版，再运行脚本与构建检查；避免分别手改三处图标。

WorkBuddy 连接器的图标更新随 0.1.2 包提交审核；仓库图标变化不会自动更新已发布的市场版本。Android 应用图标随 0.5.6 包分发；旧签名不同的调试包不能被正式签名包直接覆盖。
