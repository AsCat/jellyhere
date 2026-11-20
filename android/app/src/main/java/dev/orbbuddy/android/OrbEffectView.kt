package dev.orbbuddy.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.os.PowerManager
import android.os.SystemClock
import android.view.View
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.sin

/** Work props and speech live in a non-touchable window above the character. */
internal enum class OrbWorkEffect(val durationMs: Long) {
    LOOK(1900L), TAP(1300L),
    SWIPE_UP(1700L), SWIPE_DOWN(1700L), SWIPE_LEFT(1700L), SWIPE_RIGHT(1700L),
    TYPE(2200L);

    companion object { const val DONE_HOLD_MS = 550L }
}

internal class OrbEffectView(context: Context) : View(context) {
    private val power = context.getSystemService(PowerManager::class.java)
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val paper = Color.WHITE
    private val line = Color.rgb(215, 227, 237)
    private val soft = Color.rgb(217, 233, 247)
    private val blue = Color.rgb(59, 111, 171)
    private val handFill = Color.rgb(255, 249, 233)
    private val handNail = Color.rgb(241, 207, 183)
    private val handPath = Path().apply {
        moveTo(-2.6f, 14f); lineTo(-2.6f, 3f)
        quadTo(-2.6f, 0f, 0f, 0f); quadTo(2.6f, 0f, 2.6f, 3f)
        lineTo(2.6f, 11f)
        quadTo(4.1f, 9.4f, 5.7f, 10.2f); quadTo(7.4f, 11f, 6.7f, 13.1f)
        quadTo(8.4f, 11.6f, 9.8f, 12.7f); quadTo(11.3f, 13.9f, 10.2f, 15.5f)
        quadTo(12.2f, 14.5f, 13.3f, 16.1f); quadTo(14.2f, 17.6f, 12.7f, 19.4f)
        lineTo(10.7f, 23f); quadTo(9.5f, 25f, 6.5f, 25f)
        lineTo(-2f, 25f); quadTo(-5.1f, 24.4f, -7f, 21.5f)
        lineTo(-11f, 15.4f); quadTo(-12.5f, 12.7f, -10f, 11.5f)
        quadTo(-7.8f, 10.6f, -6.2f, 12.4f); lineTo(-2.6f, 16.7f)
        close()
    }
    private val speechTail = Path().apply {
        moveTo(65f, 32f); lineTo(63f, 38f); lineTo(71f, 34f); close()
    }
    private val keySequence = intArrayOf(2, 8, 3, 11, 5, 7, 1, 10, 4, 9, 6, 0)
    private val keyCenters = Array(14) { key ->
        val row = if (key < 5) 0 else if (key < 10) 1 else 2
        val col = if (row == 0) key else if (row == 1) key - 5 else key - 10
        val width = if (row == 2) 7f else 5.6f
        val count = if (row == 2) 4 else 5
        val rowWidth = count * width + (count - 1) * 1.3f
        Pair(45f - rowWidth / 2f + col * (width + 1.3f) + width / 2f, 74.1f + row * 6f)
    }
    private var work: OrbWorkEffect? = null
    private var workStartedAt = 0L
    var speaking = false
        set(value) {
            if (field == value) return
            field = value
            invalidate()
        }
    var connected = false
        set(value) { field = value; invalidate() }

    fun play(next: OrbWorkEffect) {
        work = next
        workStartedAt = SystemClock.uptimeMillis()
        invalidate()
    }

    fun clearWork() {
        work = null
        invalidate()
    }

    fun hasWork(now: Long = SystemClock.uptimeMillis()): Boolean {
        val current = work ?: return false
        return now - workStartedAt <= current.durationMs + OrbWorkEffect.DONE_HOLD_MS
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        if (!power.isInteractive) return
        val now = SystemClock.uptimeMillis()
        val current = work?.takeIf { hasWork(now) }
        if (current == null) work = null
        canvas.save()
        canvas.scale(width / 90f, height / 108f)
        if (current != null) {
            val progress = ((now - workStartedAt).toFloat() / current.durationMs).coerceIn(0f, 1f)
            val done = now - workStartedAt >= current.durationMs
            drawWork(canvas, current, progress, done)
        }
        if (speaking) drawSpeaking(canvas, now)
        // Repaint the status dot over the prop at its original character position.
        circle(canvas, 68f, 70f, 7f, paper)
        circle(canvas, 68f, 70f, 5f, if (connected) Color.rgb(49, 173, 107) else Color.rgb(242, 170, 65))
        canvas.restore()
        if (isAttachedToWindow && visibility == VISIBLE) {
            if (current != null) postInvalidateOnAnimation()
            else if (speaking) postInvalidateDelayed(33L)
        }
    }

    private fun drawWork(canvas: Canvas, current: OrbWorkEffect, t: Float, done: Boolean) {
        // Every work effect owns exactly the same 40 × 23 dp foreground slot.
        panel(canvas, 25f, 69f, 40f, 23f)
        when (current) {
            OrbWorkEffect.LOOK -> {
                screenRows(canvas)
                if (!done) rounded(canvas, 29f, 72f + 17f * t, 61f, 74f + 17f * t, 1f, blue)
            }
            OrbWorkEffect.TAP -> {
                screenRows(canvas)
                circle(canvas, 52f, 78f, 2.4f, blue)
                if (!done) {
                    val touch = pulse(t, 0f, .27f, .58f, .95f)
                    paint.style = Paint.Style.STROKE
                    paint.strokeWidth = 1.3f
                    paint.color = blue
                    paint.alpha = (touch * 180f).toInt().coerceIn(0, 255)
                    canvas.drawCircle(52f, 78f, 3f + 5f * touch, paint)
                    paint.alpha = 255
                    hand(canvas, 53.4f, 78f + (1f - touch) * 6f, .7f)
                }
            }
            OrbWorkEffect.SWIPE_UP, OrbWorkEffect.SWIPE_DOWN -> {
                val up = current == OrbWorkEffect.SWIPE_UP
                screenRows(canvas, (if (up) -1f else 1f) * t * 10f)
                if (!done) {
                    val from = if (up) 86f else 74f
                    val to = if (up) 74f else 86f
                    val y = from + (to - from) * ease(t)
                    line(canvas, 62f, from, 62f, y, blue, 1.6f)
                    val tail = if (up) 3f else -3f
                    line(canvas, 59f, to + tail, 62f, to, blue, 1.6f)
                    line(canvas, 62f, to, 65f, to + tail, blue, 1.6f)
                    hand(canvas, 52f, y, .7f)
                }
            }
            OrbWorkEffect.SWIPE_LEFT, OrbWorkEffect.SWIPE_RIGHT -> {
                val left = current == OrbWorkEffect.SWIPE_LEFT
                screenRows(canvas)
                if (!done) {
                    val from = if (left) 58f else 40f
                    val to = if (left) 40f else 58f
                    val x = from + (to - from) * ease(t)
                    line(canvas, from, 74f, x, 74f, blue, 1.6f)
                    val tail = if (left) 3f else -3f
                    line(canvas, to + tail, 71f, to, 74f, blue, 1.6f)
                    line(canvas, to, 74f, to + tail, 77f, blue, 1.6f)
                    hand(canvas, x, 77f, .7f)
                }
            }
            OrbWorkEffect.TYPE -> drawKeyboard(canvas, t, done)
        }
        if (done) check(canvas, 61f, 73f)
    }

    private fun drawKeyboard(canvas: Canvas, t: Float, done: Boolean) {
        val step = (t * keySequence.size).toInt().coerceIn(0, keySequence.lastIndex)
        val lit = if (done) -1 else keySequence[step]
        var key = 0
        for (row in 0..2) {
            val count = if (row == 2) 4 else 5
            val keyWidth = if (row == 2) 7f else 5.6f
            val gap = 1.3f
            val rowWidth = count * keyWidth + (count - 1) * gap
            for (col in 0 until count) {
                val x = 45f - rowWidth / 2f + col * (keyWidth + gap)
                val y = 72f + row * 6f
                rounded(canvas, x, y, x + keyWidth, y + 4.2f, 1.1f, if (key == lit) blue else soft)
                key++
            }
        }
        if (done) return
        val previous = keyCenters[keySequence[(step - 1).coerceAtLeast(0)]]
        val next = keyCenters[keySequence[step]]
        val fraction = ease(t * keySequence.size - step)
        val x = previous.first + (next.first - previous.first) * fraction
        val y = previous.second + (next.second - previous.second) * fraction -
            sin(fraction * PI).toFloat() * 1.2f
        hand(canvas, x, y, .62f)
    }

    private fun drawSpeaking(canvas: Canvas, now: Long) {
        // Speech occupies the upper corner, so it remains visible with a work prop.
        panel(canvas, 62f, 15f, 25f, 20f, radius = 7f)
        paint.style = Paint.Style.FILL
        paint.color = paper
        canvas.drawPath(speechTail, paint)
        for (i in 0..2) {
            val bar = 4f + 6f * abs(sin(now / 150.0 + i * 1.5)).toFloat()
            rounded(canvas, 68f + i * 6f, 25f - bar / 2f, 71f + i * 6f, 25f + bar / 2f, 1.5f, blue)
        }
    }

    private fun screenRows(canvas: Canvas, shift: Float = 0f) {
        canvas.save()
        canvas.clipRect(27f, 71f, 63f, 90f)
        for (row in -1..5) {
            val y = 73f + row * 5f + shift
            rounded(canvas, 30f, y, if (row % 3 == 0) 48f else 57f, y + 2f, 1f, soft)
        }
        canvas.restore()
    }

    private fun hand(canvas: Canvas, x: Float, y: Float, scale: Float) {
        canvas.save()
        canvas.translate(x, y)
        canvas.scale(scale, scale)
        paint.style = Paint.Style.FILL
        paint.color = handFill
        canvas.drawPath(handPath, paint)
        paint.style = Paint.Style.STROKE
        paint.strokeWidth = 1.5f
        paint.strokeJoin = Paint.Join.ROUND
        paint.strokeCap = Paint.Cap.ROUND
        paint.color = blue
        canvas.drawPath(handPath, paint)
        paint.style = Paint.Style.FILL
        paint.color = handNail
        canvas.drawOval(-1.45f, 1.5f, 1.45f, 5.9f, paint)
        rounded(canvas, -3f, 22f, 10f, 24.5f, 1.2f, soft)
        canvas.restore()
    }

    private fun check(canvas: Canvas, x: Float, y: Float) {
        circle(canvas, x, y, 4f, Color.rgb(49, 173, 107))
        line(canvas, x - 1.8f, y, x - .3f, y + 1.3f, paper, 1.4f)
        line(canvas, x - .3f, y + 1.3f, x + 2f, y - 1.6f, paper, 1.4f)
    }

    private fun panel(canvas: Canvas, x: Float, y: Float, w: Float, h: Float, radius: Float = 4f) {
        rounded(canvas, x, y, x + w, y + h, radius, paper)
        paint.style = Paint.Style.STROKE
        paint.strokeWidth = 1.2f
        paint.color = line
        canvas.drawRoundRect(x, y, x + w, y + h, radius, radius, paint)
    }

    private fun rounded(canvas: Canvas, left: Float, top: Float, right: Float, bottom: Float, radius: Float, color: Int) {
        paint.style = Paint.Style.FILL
        paint.color = color
        canvas.drawRoundRect(left, top, right, bottom, radius, radius, paint)
    }

    private fun line(canvas: Canvas, x1: Float, y1: Float, x2: Float, y2: Float, color: Int, width: Float) {
        paint.style = Paint.Style.STROKE
        paint.strokeWidth = width
        paint.strokeCap = Paint.Cap.ROUND
        paint.color = color
        canvas.drawLine(x1, y1, x2, y2, paint)
    }

    private fun circle(canvas: Canvas, x: Float, y: Float, radius: Float, color: Int) {
        paint.style = Paint.Style.FILL
        paint.color = color
        canvas.drawCircle(x, y, radius, paint)
    }

    private fun ease(value: Float): Float {
        val t = value.coerceIn(0f, 1f)
        return t * t * (3f - 2f * t)
    }

    private fun pulse(t: Float, a: Float, b: Float, c: Float, d: Float): Float =
        ease((t - a) / (b - a)) * (1f - ease((t - c) / (d - c)))
}
