# 果冻品牌图标

确认的主图标是暖黄色“星星果冻”配清爽蓝 `#3B6FAB`。矢量母版位于 [`assets/brand/jellyhere-icon.svg`](../assets/brand/jellyhere-icon.svg)；网站和 WorkBuddy 使用蓝底版本。Android 桌面图标使用从同一母版生成的[白底版本](../assets/brand/jellyhere-icon-android.svg)，以便在蓝色壁纸上辨认；[对比预览](images/jellyhere-android-icon-white-preview.png)展示了两者的差别。手机悬浮角色沿用现有的可动画星星实现，新安装用户默认选择它；已保存其他角色偏好的用户不会被覆盖。

在 macOS 安装 `librsvg`，或在 Linux 安装 `librsvg2-bin` 后，从仓库根目录运行：

```bash
python3 scripts/sync-brand-assets.py
python3 scripts/sync-brand-assets.py --check
```

脚本同步 WorkBuddy `icon.svg`、Go 服务内嵌的 `/brand-icon.svg`，并生成 Android 白底矢量衍生版、adaptive icon 前景图和各密度的桌面图标。Android 通知栏使用单色星形轮廓，这是系统状态栏对小图标的显示形式。若调整配色或造型，先编辑矢量母版，再运行脚本与构建检查；避免分别手改三处图标。

WorkBuddy 连接器的图标更新随 0.1.2 包提交审核；仓库图标变化不会自动更新已发布的市场版本。Android 白底图标目前随 0.5.7-rc1 试用包验收；正式发布仍以 0.5.6 为准。旧签名不同的调试包不能被正式签名包直接覆盖。
