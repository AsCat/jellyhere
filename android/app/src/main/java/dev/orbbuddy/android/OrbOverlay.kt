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

/** An optional, locally controlled accessibility overlay. Codes still require server pairing. */
class OrbOverlay(
    private val service: OrbAccessibilityService,
    private val onPauseToggle: () -> Unit,
    private val onAPIRevoke: () -> Unit,
    private val onHide: () -> Unit,
    private val onPairCodeRequest: (String) -> PairingCode.Result,
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
    private var menu: LinearLayout? = null
    private var bubble: LinearLayout? = null
    private var bubbleTitle: TextView? = null
    private var bubbleDetail: TextView? = null
    private var bubbleAction: TextView? = null
    private var positionAnimator: ValueAnimator? = null
    private var menuStatus: TextView? = null
    private var pauseAction: TextView? = null
    private var codeLine: TextView? = null
    private var menuNotice = ""
    private var receiverRegistered = false
    private var captureHidden = false
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
    private val dismissBubble = Runnable { hideBubble(); schedulePeek() }
    private val codeTicker = object : Runnable {
        override fun run() {
            if (menu == null) return
            refreshCodeLine()
            handler.postDelayed(this, 1000L)
        }
    }

    private val screenReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            orb?.invalidate()
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
            contentDescription = "果冻悬浮球，${if (paused) "已暂停" else if (apiActive) "HTTPS 远程访问中" else status}。点击可展开，展开后再次点击打开会话控制；长按开启无线调试，拖动可移动"
            invalidate()
        }
        menuStatus?.text = statusLabel()
        pauseAction?.text = if (paused) "继续远程访问" else "暂停远程访问"
        bubbleAction?.text = if (paused) "恢复" else "暂停"
        refreshCodeLine()
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
            bubble?.visibility = View.INVISIBLE
        }
    }

    fun restoreAfterCapture() {
        if (captureHidden) {
            captureHidden = false
            orb?.visibility = View.VISIBLE
            bubble?.visibility = View.VISIBLE
        }
    }

    fun react(reaction: OrbReaction, direction: Float = 1f) {
        if (!paused && !captureHidden) orb?.react(reaction, direction)
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
        paused && apiActive -> "HTTPS 已授权 · 暂停中"
        paused -> "远程访问已暂停"
        apiActive -> "HTTPS 远程访问中"
        else -> "果冻 · $connectionStatus"
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
                    params.y = (startY + event.rawY - downY).toInt().coerceIn(0, (screen.y - size).coerceAtLeast(0))
                    orb?.let { manager.updateViewLayout(it, params) }
                    hideMenu()
                    hideBubble()
                }
                return true
            }
            MotionEvent.ACTION_UP -> {
                val endedAway = abs(event.rawX - downX) > touchSlop || abs(event.rawY - downY) > touchSlop
                if (dragging && endedAway && event.eventTime - downAt >= 100L) {
                    val screen = displaySize()
                    params.x = expandedX(params.x + size / 2 < screen.x / 2, screen)
                    orb?.let { manager.updateViewLayout(it, params) }
                    prefs.edit().putInt("orb_x", params.x).putInt("orb_y", params.y).apply()
                    schedulePeek()
                } else if (!dragging && !endedAway && event.eventTime - downAt >= ViewConfiguration.getLongPressTimeout()) {
                    onWirelessDebugRequest()
                } else {
                    params.x = startX
                    params.y = startY
                    orb?.let { manager.updateViewLayout(it, params) }
                    if (isDocked()) {
                        showBubble(statusLabel(), "点星星可打开会话控制", 4000L)
                    } else toggleMenu()
                }
                return true
            }
            MotionEvent.ACTION_CANCEL -> return true
        }
        return false
    }

    private fun toggleMenu() {
        if (menu != null) {
            hideMenu()
            schedulePeek()
            return
        }
        handler.removeCallbacks(autoPeek)
        hideBubble()
        reveal()
        val params = orbParams ?: return
        val screen = displaySize()
        val width = dp(236)
        val x = (if (params.x < screen.x / 2) params.x else params.x + size - width)
            .coerceIn(0, (screen.x - width).coerceAtLeast(0))
        val hasRevokeAction = apiActive || PairingCode.active(service) != null
        val menuHeight = dp(if (hasRevokeAction) 413 else 365)
        val y = (if (params.y > menuHeight + dp(10)) params.y - menuHeight else params.y + size + dp(8))
            .coerceIn(0, (screen.y - menuHeight).coerceAtLeast(0))
        val panel = LinearLayout(service).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(14), dp(9), dp(14), dp(9))
            background = GradientDrawable().apply {
                setColor(Color.rgb(248, 252, 255))
                cornerRadius = dp(18).toFloat()
                setStroke(dp(1), Color.rgb(191, 228, 241))
            }
            elevation = dp(8).toFloat()
        }
        menuStatus = menuLine(statusLabel(), false).also(panel::addView)
        codeLine = TextView(service).apply {
            textSize = 20f
            setTextColor(Color.rgb(17, 80, 112))
            typeface = android.graphics.Typeface.MONOSPACE
            gravity = Gravity.CENTER_VERTICAL
            minHeight = dp(45)
        }.also(panel::addView)
        refreshCodeLine()
        menuLine("生成只读授权码", true).also {
            it.setOnClickListener { generateCode("viewer") }
            panel.addView(it)
        }
        menuLine("生成测试控制码", true).also {
            it.setOnClickListener { generateCode("controller") }
            panel.addView(it)
        }
        menuLine("开启无线调试", true).also {
            it.setOnClickListener {
                hideMenu()
                onWirelessDebugRequest()
            }
            panel.addView(it)
        }
        pauseAction = menuLine(if (paused) "继续远程访问" else "暂停远程访问", true).also {
            it.setOnClickListener {
                hideMenu()
                onPauseToggle()
            }
            panel.addView(it)
        }
        if (hasRevokeAction) menuLine(if (apiActive) "撤销 HTTPS 授权" else "清除当前授权码", true).also {
            it.setOnClickListener {
                hideMenu()
                onAPIRevoke()
            }
            panel.addView(it)
        }
        menuLine("隐藏悬浮球", true).also {
            it.setOnClickListener {
                hideMenu()
                onHide()
            }
            panel.addView(it)
        }
        try {
            manager.addView(panel, layoutParams(width, WindowManager.LayoutParams.WRAP_CONTENT, x, y))
            menu = panel
            handler.postDelayed(codeTicker, 1000L)
        } catch (e: RuntimeException) {
            menuStatus = null
            pauseAction = null
            codeLine = null
            Log.w("JellyHere", "cannot show orb controls", e)
        }
    }

    private fun generateCode(scope: String) {
        hideMenu()
        val result = onPairCodeRequest(scope)
        menuNotice = if (result.code == null) result.message else ""
        toggleMenu()
    }

    private fun refreshCodeLine() {
        val active = PairingCode.active(service)
        codeLine?.textSize = if (active == null) 13f else 20f
        codeLine?.text = when {
            active != null -> "${if (active.scope == "controller") "控制" else "只读"} ${active.code} · ${active.secondsLeft} 秒"
            menuNotice.isNotEmpty() -> menuNotice
            else -> "当前无有效授权码"
        }
    }

    private fun hideMenu() {
        handler.removeCallbacks(codeTicker)
        menu?.let { runCatching { manager.removeView(it) } }
        menu = null
        menuStatus = null
        pauseAction = null
        codeLine = null
        menuNotice = ""
    }

    private fun isDocked(): Boolean {
        val params = orbParams ?: return false
        val screen = displaySize()
        return params.x < 0 || params.x > screen.x - size
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
        if (orb != null && menu == null) handler.postDelayed(autoPeek, 4200L)
    }

    private fun showBubble(title: String, detail: String, durationMs: Long) {
        val params = orbParams ?: return
        hideMenu()
        hideBubble()
        reveal()
        handler.removeCallbacks(autoPeek)
        val screen = displaySize()
        val width = dp(203)
        val left = params.x + size / 2 < screen.x / 2
        val x = if (left) size + edge else screen.x - size - width - edge
        val y = (params.y - dp(12)).coerceIn(edge, (screen.y - dp(108)).coerceAtLeast(edge))
        val panel = LinearLayout(service).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(13), dp(9), dp(13), dp(9))
            background = GradientDrawable().apply {
                setColor(Color.rgb(255, 252, 245))
                cornerRadius = dp(17).toFloat()
                setStroke(dp(1).coerceAtLeast(1), Color.rgb(242, 207, 146))
            }
            elevation = dp(10).toFloat()
        }
        bubbleTitle = TextView(service).apply {
            text = title
            textSize = 14f
            setTextColor(Color.rgb(91, 65, 45))
            setTypeface(null, android.graphics.Typeface.BOLD)
        }.also(panel::addView)
        bubbleDetail = TextView(service).apply {
            text = detail
            textSize = 11f
            setTextColor(Color.rgb(130, 104, 84))
        }.also(panel::addView)
        bubbleAction = menuLine(if (paused) "恢复" else "暂停", true).apply {
            textSize = 13f
            minHeight = dp(35)
            setTextColor(Color.rgb(172, 86, 39))
            setOnClickListener { onPauseToggle() }
        }.also(panel::addView)
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
