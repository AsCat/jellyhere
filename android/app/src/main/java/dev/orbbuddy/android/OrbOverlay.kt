package dev.orbbuddy.android

import android.animation.ValueAnimator
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.graphics.Color
import android.graphics.PixelFormat
import android.graphics.Point
import android.graphics.drawable.GradientDrawable
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.Gravity
import android.view.MotionEvent
import android.view.View
import android.view.ViewConfiguration
import android.view.WindowManager
import android.view.animation.DecelerateInterpolator
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import kotlin.math.abs

/** An optional, locally controlled accessibility overlay. */
class OrbOverlay(
    private val service: OrbAccessibilityService,
    private val onPauseToggle: () -> Unit,
    private val onAPIRevoke: () -> Unit,
    private val onHide: () -> Unit,
    private val onWirelessDebugRequest: () -> Unit,
) {
    private val manager = service.getSystemService(WindowManager::class.java)
    private val prefs = service.getSharedPreferences("connection", Context.MODE_PRIVATE)
    private val handler = Handler(Looper.getMainLooper())
    private val density = service.resources.displayMetrics.density
    private val size = dp(72)
    private val edge = dp(12)
    private val visiblePart = dp(27)
    private val touchSlop = ViewConfiguration.get(service).scaledTouchSlop
    private var orb: OrbFaceView? = null
    private var orbParams: WindowManager.LayoutParams? = null
    private var effectView: OrbEffectView? = null
    private var effectParams: WindowManager.LayoutParams? = null
    private var menu: LinearLayout? = null
    private var bubble: LinearLayout? = null
    private var bubbleTitle: TextView? = null
    private var bubbleDetail: TextView? = null
    private var bubbleAction: TextView? = null
    private var positionAnimator: ValueAnimator? = null
    private var menuMore = false
    private var receiverRegistered = false
    private var captureHidden = false
    private var speaking = false
    private var paused = false
    private var connectionStatus = "连接中"
    private var apiActive = false
    private var downX = 0f
    private var downY = 0f
    private var startX = 0
    private var startY = 0
    private var downAt = 0L
    private var dragging = false
    private var activeRemoteLabel: String? = null
    private var activeRemoteId: String? = null
    private val autoPeek = Runnable { if (menu == null && !captureHidden) dock() }
    private val dismissMenu = Runnable { hideMenu(); schedulePeek() }
    private val dismissBubble = Runnable { hideBubble(); schedulePeek() }
    private val dismissEffect = Runnable {
        effectView?.clearWork()
        if (!speaking) removeEffect()
    }

    private val screenReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            orb?.invalidate()
            effectView?.invalidate()
        }
    }

    fun sync(show: Boolean, paused: Boolean, status: String, apiActive: Boolean) {
        val pauseChanged = this.paused != paused
        this.paused = paused
        connectionStatus = status
        this.apiActive = apiActive
        if (show) show() else hide()
        orb?.apply {
            this.paused = paused
            this.connected = status == "已连接"
            this.character = OrbCharacter.from(prefs.getString("orb_character", null))
            contentDescription = "果冻悬浮球，${if (paused) "已暂停" else status}。点击打开控制菜单，拖动可移动"
            invalidate()
        }
        effectView?.connected = status == "已连接"
        if (paused) {
            speaking = false
            removeEffect()
        }
        if (menu != null) {
            val more = menuMore
            hideMenu()
            showMenu(more)
        }
        bubbleAction?.text = if (paused) "恢复" else "暂停"
        if (pauseChanged && show) {
            activeRemoteLabel = null
            activeRemoteId = null
            showBubble(if (paused) "远程访问已暂停" else "远程访问已恢复",
                if (paused) "后续请求已阻止" else "可以继续开发测试", 3500L)
        }
    }

    fun hideForCapture() {
        if (orb != null) {
            hideMenu()
            captureHidden = true
            orb?.visibility = View.INVISIBLE
            effectView?.visibility = View.INVISIBLE
            bubble?.visibility = View.INVISIBLE
        }
    }

    fun restoreAfterCapture() {
        if (captureHidden) {
            captureHidden = false
            orb?.visibility = View.VISIBLE
            effectView?.visibility = View.VISIBLE
            bubble?.visibility = View.VISIBLE
        }
    }

    fun react(reaction: OrbReaction, direction: Float = 1f, vertical: Boolean = false) {
        if (paused || captureHidden || orb == null) return
        orb?.react(reaction, direction, vertical)
        val work = when (reaction) {
            OrbReaction.LOOK -> OrbWorkEffect.LOOK
            OrbReaction.TAP -> OrbWorkEffect.TAP
            OrbReaction.TYPE -> OrbWorkEffect.TYPE
            OrbReaction.SWIPE -> when {
                vertical && direction < 0 -> OrbWorkEffect.SWIPE_UP
                vertical -> OrbWorkEffect.SWIPE_DOWN
                direction < 0 -> OrbWorkEffect.SWIPE_LEFT
                else -> OrbWorkEffect.SWIPE_RIGHT
            }
            OrbReaction.KEY -> null
        } ?: return
        reveal()
        ensureEffect()?.play(work)
        handler.removeCallbacks(dismissEffect)
        handler.postDelayed(dismissEffect, work.durationMs + OrbWorkEffect.DONE_HOLD_MS + 50L)
        schedulePeek()
    }

    /** Voice playback calls this independently of work actions. */
    fun setSpeaking(active: Boolean) {
        speaking = active && !paused && orb != null
        if (speaking) {
            reveal()
            handler.removeCallbacks(autoPeek)
            ensureEffect()?.speaking = true
        } else {
            effectView?.speaking = false
            if (effectView?.hasWork() != true) removeEffect()
            schedulePeek()
        }
    }

    fun beginRemoteAction(id: String, label: String) {
        if (paused || orb == null) return
        activeRemoteId = id
        activeRemoteLabel = label
        showBubble("正在$label", "可随时暂停后续操作", 0L)
    }

    fun finishRemoteAction(id: String, success: Boolean) {
        if (activeRemoteId != id) return
        val label = activeRemoteLabel ?: return
        activeRemoteId = null
        activeRemoteLabel = null
        if (paused || orb == null) return
        showBubble(if (success) "已$label" else "$label 未完成",
            if (success) "操作结果已返回" else "请查看智能体收到的错误", 2600L)
    }

    fun showMessage(message: String) {
        Toast.makeText(service, message, Toast.LENGTH_LONG).show()
    }

    fun destroy() = hide()

    private fun statusLabel() = when {
        paused -> "果冻 · 已暂停"
        activeRemoteLabel != null -> "果冻 · 正在操作"
        else -> "果冻 · $connectionStatus"
    }

    private fun statusDetail() = when {
        paused -> "后续读屏与操作已阻止"
        activeRemoteLabel != null -> "正在${activeRemoteLabel}，可暂停后续操作"
        connectionStatus == "已连接" && apiActive -> "临时授权正在使用"
        connectionStatus == "已连接" -> "当前空闲，等待智能体"
        else -> "打开果冻查看连接状态"
    }

    private fun show() {
        if (orb != null) return
        val screen = displaySize()
        val savedX = prefs.getInt("orb_x", screen.x - size - edge)
        val x = dockX(savedX < screen.x / 2, screen)
        val y = prefs.getInt("orb_y", screen.y * 2 / 3).coerceIn(edge, (screen.y - size - edge).coerceAtLeast(edge))
        val params = layoutParams(size, size, x, y)
        params.flags = params.flags or WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS
        val view = OrbFaceView(service).apply {
            character = OrbCharacter.from(prefs.getString("orb_character", null))
        }
        view.setOnTouchListener { _, event -> onTouch(event) }
        try {
            manager.addView(view, params)
            orb = view
            orbParams = params
            schedulePeek()
            if (!receiverRegistered) {
                val filter = IntentFilter().apply {
                    addAction(Intent.ACTION_SCREEN_ON)
                    addAction(Intent.ACTION_SCREEN_OFF)
                }
                if (Build.VERSION.SDK_INT >= 33) {
                    service.registerReceiver(screenReceiver, filter, Context.RECEIVER_NOT_EXPORTED)
                } else {
                    @Suppress("DEPRECATION")
                    service.registerReceiver(screenReceiver, filter)
                }
                receiverRegistered = true
            }
        } catch (e: RuntimeException) {
            Log.w("JellyHere", "cannot show floating orb", e)
        }
    }

    private fun hide() {
        handler.removeCallbacks(autoPeek)
        handler.removeCallbacks(dismissBubble)
        speaking = false
        removeEffect()
        positionAnimator?.cancel()
        positionAnimator = null
        activeRemoteLabel = null
        activeRemoteId = null
        hideBubble()
        hideMenu()
        orb?.let { runCatching { manager.removeView(it) } }
        orb = null
        orbParams = null
        captureHidden = false
        if (receiverRegistered) {
            runCatching { service.unregisterReceiver(screenReceiver) }
            receiverRegistered = false
        }
    }

    private fun onTouch(event: MotionEvent): Boolean {
        val params = orbParams ?: return false
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                handler.removeCallbacks(autoPeek)
                positionAnimator?.cancel()
                downX = event.rawX
                downY = event.rawY
                startX = params.x
                startY = params.y
                downAt = event.eventTime
                dragging = false
                return true
            }
            MotionEvent.ACTION_MOVE -> {
                if (abs(event.rawX - downX) > touchSlop || abs(event.rawY - downY) > touchSlop) dragging = true
                if (dragging) {
                    val screen = displaySize()
                    params.x = (startX + event.rawX - downX).toInt().coerceIn(-size + visiblePart, (screen.x - visiblePart).coerceAtLeast(0))
                    params.y = (startY + event.rawY - downY).toInt()
                        .coerceIn(edge, (screen.y - size - edge).coerceAtLeast(edge))
                    orb?.let { manager.updateViewLayout(it, params) }
                    syncEffectPosition()
                    hideMenu()
                    hideBubble()
                }
                return true
            }
            MotionEvent.ACTION_UP -> {
                val endedAway = abs(event.rawX - downX) > touchSlop || abs(event.rawY - downY) > touchSlop
                if (dragging && endedAway && event.eventTime - downAt >= 100L) {
                    val screen = displaySize()
                    val left = params.x + size / 2 < screen.x / 2
                    prefs.edit().putInt("orb_x", expandedX(left, screen)).putInt("orb_y", params.y).apply()
                    dock()
                } else {
                    params.x = startX
                    params.y = startY
                    orb?.let { manager.updateViewLayout(it, params) }
                    syncEffectPosition()
                    toggleMenu()
                }
                return true
            }
            MotionEvent.ACTION_CANCEL -> {
                // Edge gestures may cancel a drag before ACTION_UP; never leave the orb stranded.
                dock()
                return true
            }
        }
        return false
    }

    private fun toggleMenu() {
        if (menu != null) { hideMenu(); schedulePeek() } else showMenu(false)
    }

    private fun showMenu(more: Boolean) {
        handler.removeCallbacks(autoPeek)
        hideBubble()
        reveal()
        val params = orbParams ?: return
        val screen = displaySize()
        val width = dp(226)
        val left = params.x + size / 2 < screen.x / 2
        val x = (if (left) dp(8) else screen.x - width - dp(8))
            .coerceIn(0, (screen.x - width).coerceAtLeast(0))
        val panel = LinearLayout(service).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(10), dp(10), dp(10), dp(10))
            background = GradientDrawable().apply {
                setColor(Color.rgb(255, 252, 246))
                cornerRadius = dp(18).toFloat()
                setStroke(dp(1), Color.rgb(240, 214, 171))
            }
            elevation = dp(8).toFloat()
        }
        if (more) {
            addMenuAction(panel, "‹  返回控制面板") { hideMenu(); showMenu(false) }
            if (apiActive || PairingCode.active(service) != null) {
                addMenuAction(panel, "撤销临时授权") { hideMenu(); onAPIRevoke() }
            }
            addMenuAction(panel, "收起到边缘") { hideMenu(); dock() }
            addMenuAction(panel, "隐藏悬浮角色") { hideMenu(); onHide() }
            addMenuAction(panel, "打开果冻") { openApp() }
        } else {
            val header = LinearLayout(service).apply { orientation = LinearLayout.HORIZONTAL; gravity = Gravity.CENTER_VERTICAL }
            val statusText = TextView(service).apply {
                text = "●  ${statusLabel()}"
                textSize = 13f
                setTextColor(if (paused) Color.GRAY else Color.rgb(77, 89, 62))
                setTypeface(null, android.graphics.Typeface.BOLD)
                gravity = Gravity.CENTER_VERTICAL
            }
            header.addView(statusText, LinearLayout.LayoutParams(0, dp(44), 1f))
            val moreButton = menuAction("⋯", false).apply {
                textSize = 20f
                contentDescription = "更多选项"
                background = null
                setOnClickListener { hideMenu(); showMenu(true) }
            }
            header.addView(moreButton, LinearLayout.LayoutParams(dp(44), dp(44)))
            panel.addView(header)
            TextView(service).apply {
                text = statusDetail()
                textSize = 12f
                setTextColor(Color.rgb(112, 124, 143))
                gravity = Gravity.CENTER_VERTICAL
                minHeight = dp(28)
            }.also(panel::addView)
            val connected = connectionStatus == "已连接"
            val actionLabel = when {
                !connected -> "查看连接问题"
                paused -> "恢复远程访问"
                activeRemoteLabel != null -> "暂停后续操作"
                else -> "暂停远程访问"
            }
            addMenuAction(panel, actionLabel, true) {
                hideMenu()
                if (connected) onPauseToggle() else openApp()
            }
            val row = LinearLayout(service).apply { orientation = LinearLayout.HORIZONTAL }
            if (connected && !paused && activeRemoteLabel == null) {
                val quick = menuAction("无线调试", false).apply {
                    contentDescription = "打开无线调试设置"
                    setOnClickListener { hideMenu(); onWirelessDebugRequest() }
                }
                row.addView(quick, LinearLayout.LayoutParams(0, dp(48), 1f).apply { rightMargin = dp(6) })
            }
            val console = consoleURL()
            val destination = menuAction(if (console != null) "打开控制台" else "打开果冻", false).apply {
                contentDescription = if (console != null) "打开设备空间控制台" else "打开果冻 App"
                setOnClickListener { if (console != null) openConsole(console) else openApp() }
            }
            row.addView(destination, LinearLayout.LayoutParams(0, dp(48), 1f))
            panel.addView(row, LinearLayout.LayoutParams(-1, dp(48)).apply { topMargin = dp(6) })
        }
        panel.measure(View.MeasureSpec.makeMeasureSpec(width, View.MeasureSpec.EXACTLY),
            View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
        val menuHeight = panel.measuredHeight
        val above = params.y - menuHeight - dp(8)
        val y = (if (above >= dp(8)) above else params.y + size + dp(8))
            .coerceIn(0, (screen.y - menuHeight).coerceAtLeast(0))
        try {
            manager.addView(panel, layoutParams(width, WindowManager.LayoutParams.WRAP_CONTENT, x, y))
            menu = panel
            menuMore = more
            handler.postDelayed(dismissMenu, 20_000L)
        } catch (e: RuntimeException) {
            Log.w("JellyHere", "cannot show orb controls", e)
        }
    }

    private fun menuAction(label: String, prominent: Boolean): TextView = menuLine(label, true).apply {
        textSize = 14f
        gravity = Gravity.CENTER
        minHeight = dp(48)
        setTextColor(if (prominent) Color.WHITE else Color.rgb(57, 75, 107))
        background = GradientDrawable().apply {
            setColor(if (prominent) Color.rgb(66, 92, 183) else Color.WHITE)
            cornerRadius = dp(11).toFloat()
            if (!prominent) setStroke(dp(1), Color.rgb(226, 231, 240))
        }
    }

    private fun addMenuAction(panel: LinearLayout, label: String, prominent: Boolean = false,
        action: () -> Unit): TextView = menuAction(label, prominent).also {
        it.setOnClickListener { action() }
        panel.addView(it, LinearLayout.LayoutParams(-1, dp(48)).apply { topMargin = dp(6) })
    }

    private fun openApp() {
        hideMenu()
        runCatching {
            service.startActivity(Intent(service, MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP or
                    Intent.FLAG_ACTIVITY_SINGLE_TOP))
        }.onFailure { showMessage("无法打开果冻：${it.message}") }
    }

    private fun consoleURL(): Uri? {
        val managed = prefs.getString("connection_mode", null) == "managed" ||
            (!prefs.contains("connection_mode") &&
                (prefs.getString("device_id", "") ?: "").startsWith("phone_"))
        if (!managed) return null
        val deviceURL = runCatching { Uri.parse(prefs.getString("url", "") ?: "") }.getOrNull()
            ?: return null
        if (deviceURL.scheme != "wss" || deviceURL.host.isNullOrBlank()) return null
        return deviceURL.buildUpon().scheme("https").encodedPath("/").encodedQuery(null)
            .fragment(null).build()
    }

    private fun openConsole(url: Uri) {
        hideMenu()
        runCatching {
            service.startActivity(Intent(Intent.ACTION_VIEW, url).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }.onFailure { showMessage("无法打开控制台：${it.message}") }
    }

    private fun hideMenu() {
        handler.removeCallbacks(dismissMenu)
        menu?.let { runCatching { manager.removeView(it) } }
        menu = null
        menuMore = false
    }

    private fun expandedX(left: Boolean, screen: Point) = if (left) edge else screen.x - size - edge

    private fun dockX(left: Boolean, screen: Point) = if (left) -(size - visiblePart) else screen.x - visiblePart

    private fun animateX(target: Int) {
        val params = orbParams ?: return
        val view = orb ?: return
        positionAnimator?.cancel()
        if (params.x == target) return
        positionAnimator = ValueAnimator.ofInt(params.x, target).apply {
            duration = 260L
            interpolator = DecelerateInterpolator()
            addUpdateListener {
                params.x = it.animatedValue as Int
                runCatching { manager.updateViewLayout(view, params) }
                syncEffectPosition()
            }
            start()
        }
    }

    private fun reveal() {
        val params = orbParams ?: return
        val screen = displaySize()
        animateX(expandedX(params.x + size / 2 < screen.x / 2, screen))
    }

    private fun dock() {
        val params = orbParams ?: return
        val screen = displaySize()
        animateX(dockX(params.x + size / 2 < screen.x / 2, screen))
    }

    private fun schedulePeek() {
        handler.removeCallbacks(autoPeek)
        if (orb != null && menu == null && !speaking) handler.postDelayed(autoPeek, 4200L)
    }

    private fun ensureEffect(): OrbEffectView? {
        effectView?.let { return it }
        if (orbParams == null) return null
        val view = OrbEffectView(service).apply {
            connected = connectionStatus == "已连接"
            speaking = this@OrbOverlay.speaking
            if (captureHidden) visibility = View.INVISIBLE
        }
        val params = layoutParams(dp(90), dp(108), 0, 0).apply {
            flags = flags or WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE or
                WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS
        }
        effectView = view
        effectParams = params
        positionEffectParams()
        return try {
            manager.addView(view, params)
            view
        } catch (e: RuntimeException) {
            effectView = null
            effectParams = null
            Log.w("JellyHere", "cannot show orb work effect", e)
            null
        }
    }

    private fun positionEffectParams() {
        val orbPosition = orbParams ?: return
        val params = effectParams ?: return
        params.x = orbPosition.x - dp(9)
        params.y = orbPosition.y - dp(12)
    }

    private fun syncEffectPosition() {
        val view = effectView ?: return
        val params = effectParams ?: return
        val oldX = params.x
        val oldY = params.y
        positionEffectParams()
        if (params.x != oldX || params.y != oldY) {
            runCatching { manager.updateViewLayout(view, params) }
        }
    }

    private fun removeEffect() {
        handler.removeCallbacks(dismissEffect)
        effectView?.let { runCatching { manager.removeView(it) } }
        effectView = null
        effectParams = null
    }

    private fun showBubble(title: String, detail: String, durationMs: Long) {
        val params = orbParams ?: return
        hideMenu()
        hideBubble()
        reveal()
        handler.removeCallbacks(autoPeek)
        val screen = displaySize()
        val width = dp(160)
        val left = params.x + size / 2 < screen.x / 2
        val x = if (left) dp(8) else screen.x - width - dp(8)
        val panel = LinearLayout(service).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(10), dp(6), dp(10), dp(6))
            background = GradientDrawable().apply {
                setColor(Color.rgb(255, 252, 245))
                cornerRadius = dp(13).toFloat()
                setStroke(dp(1).coerceAtLeast(1), Color.rgb(242, 207, 146))
            }
            elevation = dp(6).toFloat()
        }
        bubbleTitle = TextView(service).apply {
            text = title
            textSize = 12f
            setTextColor(Color.rgb(91, 65, 45))
            setTypeface(null, android.graphics.Typeface.BOLD)
        }.also(panel::addView)
        bubbleDetail = TextView(service).apply {
            text = detail
            textSize = 10f
            setTextColor(Color.rgb(130, 104, 84))
        }.also(panel::addView)
        bubbleAction = menuLine(if (paused) "恢复" else "暂停", true).apply {
            textSize = 11f
            minHeight = dp(28)
            setTextColor(Color.rgb(172, 86, 39))
            setOnClickListener { onPauseToggle() }
        }.also(panel::addView)
        panel.measure(View.MeasureSpec.makeMeasureSpec(width, View.MeasureSpec.EXACTLY),
            View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
        val bubbleHeight = panel.measuredHeight
        val gap = dp(8)
        val above = params.y - bubbleHeight - gap
        val y = if (above >= dp(8)) above else
            (params.y + size + gap).coerceAtMost((screen.y - bubbleHeight - dp(32)).coerceAtLeast(dp(8)))
        try {
            manager.addView(panel, layoutParams(width, WindowManager.LayoutParams.WRAP_CONTENT,
                x.coerceIn(0, (screen.x - width).coerceAtLeast(0)), y))
            bubble = panel
            if (captureHidden) panel.visibility = View.INVISIBLE
            if (durationMs > 0) handler.postDelayed(dismissBubble, durationMs)
        } catch (e: RuntimeException) {
            bubbleTitle = null
            bubbleDetail = null
            bubbleAction = null
            Log.w("JellyHere", "cannot show orb activity bubble", e)
        }
    }

    private fun hideBubble() {
        handler.removeCallbacks(dismissBubble)
        bubble?.let { runCatching { manager.removeView(it) } }
        bubble = null
        bubbleTitle = null
        bubbleDetail = null
        bubbleAction = null
    }

    private fun menuLine(label: String, action: Boolean) = TextView(service).apply {
        text = label
        textSize = if (action) 16f else 13f
        setTextColor(if (action) Color.rgb(17, 80, 112) else Color.rgb(78, 108, 123))
        gravity = Gravity.CENTER_VERTICAL
        minHeight = dp(if (action) 48 else 34)
        if (action) {
            isClickable = true
            isFocusable = true
            contentDescription = label
        }
    }

    private fun layoutParams(width: Int, height: Int, x: Int, y: Int) = WindowManager.LayoutParams(
        width, height, WindowManager.LayoutParams.TYPE_ACCESSIBILITY_OVERLAY,
        WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE or WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL,
        PixelFormat.TRANSLUCENT
    ).apply {
        gravity = Gravity.TOP or Gravity.START
        this.x = x
        this.y = y
    }

    private fun displaySize() = Point().also { manager.defaultDisplay.getRealSize(it) }
    private fun dp(value: Int) = (value * density).toInt()

}
