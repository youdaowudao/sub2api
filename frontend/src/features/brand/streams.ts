/**
 * “汇流”背景特效：细线从左右两侧汇入焦点（Logo），光点沿线加速流入。
 * 宽屏时部分线条从模型名标签出发。只负责画布绘制，不处理页面布局。
 */

export interface StreamsOptions {
  /** 返回焦点元素（线条汇入的位置）；找不到时汇向画布中上部 */
  focal: () => Element | null
  /** 是否在两侧显示模型名（宽度不少于 900 像素时才显示） */
  labels?: boolean
  /** 淡化模式：线条更少、更淡 */
  soft?: boolean
  models?: { left: readonly string[]; right: readonly string[] }
}

export interface StreamsHandle {
  /** 重新计算尺寸并重绘（布局变化后调用） */
  refresh(): void
  /** 停止动画并移除所有监听 */
  destroy(): void
}

type RGB = readonly [number, number, number]
interface Point { x: number; y: number }
interface Pulse { t: number; v: number }
interface Chip { name: string; x: number; w: number; ph: number; flash: number; rip: number }
interface Line {
  left: boolean
  y: number
  cy: number
  ci: number
  ph: number
  w: number
  pulses: Pulse[]
  chip?: Chip
}

const LABEL_MIN_WIDTH = 900
const LABEL_Y = [0.25, 0.4, 0.55, 0.7]
const FONT = '600 13px system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif'
const PALETTE: { light: readonly RGB[]; dark: readonly RGB[] } = {
  light: [[13, 148, 136], [8, 145, 178], [37, 99, 235]],
  dark: [[121, 244, 189], [57, 217, 231], [56, 117, 246]]
}
const TAU = Math.PI * 2

const noop: StreamsHandle = { refresh() {}, destroy() {} }

const rand = (a: number, b: number) => a + Math.random() * (b - a)

function bezier(P: Point[], t: number): Point {
  const u = 1 - t
  const a = u * u * u
  const b = 3 * u * u * t
  const c = 3 * u * t * t
  const d = t * t * t
  return {
    x: a * P[0].x + b * P[1].x + c * P[2].x + d * P[3].x,
    y: a * P[0].y + b * P[1].y + c * P[2].y + d * P[3].y
  }
}

function prefersReducedMotion(): boolean {
  try {
    return !!window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches
  } catch {
    return false
  }
}

export function createStreams(canvas: HTMLCanvasElement, options: StreamsOptions): StreamsHandle {
  let ctx: CanvasRenderingContext2D | null = null
  try {
    ctx = canvas.getContext('2d')
  } catch {
    ctx = null
  }
  if (!ctx) return noop
  const g: CanvasRenderingContext2D = ctx

  const soft = !!options.soft
  const models = options.models ?? { left: [], right: [] }
  const motion = !prefersReducedMotion()

  let W = 0
  let H = 0
  let raf = 0
  let last = 0
  let T = 0
  let flash = 0
  let lines: Line[] = []
  let resizeTimer: ReturnType<typeof setTimeout> | undefined
  let destroyed = false

  const isDark = () => document.documentElement.classList.contains('dark')
  const color = (i: number, a: number) => {
    const p = (isDark() ? PALETTE.dark : PALETTE.light)[i % 3]
    return `rgba(${p[0]},${p[1]},${p[2]},${a})`
  }
  const showLabels = () => !!options.labels && W >= LABEL_MIN_WIDTH && models.left.length + models.right.length > 0

  function roundRect(x: number, y: number, w: number, h: number, r: number) {
    g.beginPath()
    if (typeof g.roundRect === 'function') {
      g.roundRect(x, y, w, h, r)
      return
    }
    g.moveTo(x + r, y)
    g.arcTo(x + w, y, x + w, y + h, r)
    g.arcTo(x + w, y + h, x, y + h, r)
    g.arcTo(x, y + h, x, y, r)
    g.arcTo(x, y, x + w, y, r)
    g.closePath()
  }

  function focalPoint() {
    const el = options.focal()
    const box = canvas.getBoundingClientRect()
    if (el) {
      const r = el.getBoundingClientRect()
      if (r.width > 0) {
        return { x: r.left - box.left + r.width / 2, y: r.top - box.top + r.height / 2, r: r.width / 2 }
      }
    }
    return { x: W / 2, y: H * 0.38, r: 40 }
  }

  function initLines() {
    const labels = showLabels()
    const n = W < 640 ? 16 : labels ? 18 : soft ? 20 : 24
    const m = Math.ceil(n / 2)
    lines = []
    for (let i = 0; i < n; i++) {
      const k = Math.floor(i / 2)
      const line: Line = {
        left: i % 2 === 0,
        y: H * (0.04 + (0.92 * (k + 0.5)) / m) + rand(-18, 18),
        cy: 0,
        ci: i % 3,
        ph: rand(0, TAU),
        w: rand(0.6, 1.2),
        pulses: [{ t: rand(-0.9, 0.9), v: rand(0.11, 0.2) }]
      }
      if (Math.random() < 0.4) line.pulses.push({ t: rand(-1.6, -0.2), v: rand(0.11, 0.2) })
      lines.push(line)
    }
    if (labels) {
      g.font = FONT
      const pad = Math.max(28, W * 0.045)
      ;(['left', 'right'] as const).forEach((side) => {
        models[side].forEach((name, j) => {
          const cw = g.measureText(name).width + 28
          const cx = side === 'left' ? pad : W - pad - cw
          lines.push({
            left: side === 'left',
            y: H * LABEL_Y[j % LABEL_Y.length],
            cy: 0,
            ci: (j + (side === 'left' ? 0 : 1)) % 3,
            ph: 0,
            w: 1.3,
            chip: { name, x: cx, w: cw, ph: rand(0, TAU), flash: 0, rip: 1 },
            pulses: [
              { t: rand(-0.7, 0.6), v: rand(0.12, 0.16) },
              { t: rand(-1.6, -0.8), v: rand(0.12, 0.16) }
            ]
          })
        })
      })
    }
    flash = 0
  }

  function pathOf(l: Line, f: { x: number; y: number; r: number }, t: number): Point[] {
    let x0: number
    let y0: number
    if (l.chip) {
      x0 = l.left ? l.chip.x + l.chip.w : l.chip.x
      y0 = l.cy
    } else {
      x0 = l.left ? -30 : W + 30
      y0 = l.y + Math.sin(t * 0.35 + l.ph) * 14
    }
    const p1 = { x: x0 + (f.x - x0) * 0.52, y: y0 }
    const dy = Math.max(-70, Math.min(70, (y0 - f.y) * 0.22))
    const p2 = { x: f.x + (x0 - f.x) * 0.3, y: f.y + dy }
    const a = Math.atan2(p2.y - f.y, p2.x - f.x)
    const R = f.r + 30
    return [{ x: x0, y: y0 }, p1, p2, { x: f.x + Math.cos(a) * R, y: f.y + Math.sin(a) * R }]
  }

  function drawChip(l: Line, t: number, dt: number) {
    const c = l.chip as Chip
    const dark = isDark()
    const h = 30
    const y = l.cy - h / 2
    const fl = c.flash

    g.save()
    g.globalCompositeOperation = 'source-over'
    g.shadowColor = color(l.ci, (dark ? 0.6 : 0.4) * (0.3 + 0.7 * fl))
    g.shadowBlur = 10 + 18 * fl
    g.fillStyle = dark ? 'rgba(12,19,36,.86)' : 'rgba(255,255,255,.94)'
    roundRect(c.x, y, c.w, h, 15)
    g.fill()
    g.restore()

    // 边框流光
    const s = ((t * 0.22 + c.ph) % 1.7) - 0.35
    const grad = g.createLinearGradient(c.x, 0, c.x + c.w, 0)
    const base = color(l.ci, dark ? 0.42 : 0.3)
    const hi = color(l.ci, 1)
    grad.addColorStop(0, base)
    grad.addColorStop(1, base)
    if (s > 0.02 && s < 0.98) {
      grad.addColorStop(Math.max(0.01, s - 0.2), base)
      grad.addColorStop(s, hi)
      grad.addColorStop(Math.min(0.99, s + 0.2), base)
    }
    g.strokeStyle = grad
    g.lineWidth = 1.3
    roundRect(c.x + 0.5, y + 0.5, c.w - 1, h - 1, 14.5)
    g.stroke()

    // 出发点：涟漪 + 圆点
    const dx = l.left ? c.x + c.w : c.x
    if (c.rip < 1) {
      g.strokeStyle = color(l.ci, (1 - c.rip) * 0.75)
      g.lineWidth = 1.2
      g.beginPath()
      g.arc(dx, l.cy, 3 + c.rip * 18, 0, TAU)
      g.stroke()
    }
    g.fillStyle = color(l.ci, 1)
    g.beginPath()
    g.arc(dx, l.cy, 2.8 + 1.6 * fl, 0, TAU)
    g.fill()

    g.fillStyle = dark ? 'rgba(226,232,240,.95)' : 'rgba(30,41,59,.92)'
    g.font = FONT
    g.textBaseline = 'middle'
    g.textAlign = 'center'
    g.fillText(c.name, c.x + c.w / 2, l.cy + 0.5)

    c.flash *= Math.pow(0.12, dt)
    c.rip = Math.min(1, c.rip + dt * 1.3)
  }

  function draw(dt: number, t: number) {
    const dark = isDark()
    const f = focalPoint()
    const k = soft ? 0.8 : 1
    const blend: GlobalCompositeOperation = dark ? 'lighter' : 'source-over'

    g.globalCompositeOperation = blend
    const gr0 = f.r * 4.4
    const glow = g.createRadialGradient(f.x, f.y, 0, f.x, f.y, gr0)
    glow.addColorStop(0, color(1, ((dark ? 0.24 : 0.13) + flash * (dark ? 0.16 : 0.08)) * k))
    glow.addColorStop(1, color(1, 0))
    g.fillStyle = glow
    g.fillRect(f.x - gr0, f.y - gr0, gr0 * 2, gr0 * 2)
    g.lineCap = 'round'

    for (const l of lines) {
      if (l.chip) l.cy = l.y + Math.sin(t * 0.9 + l.chip.ph) * 3
      g.globalCompositeOperation = blend
      const base = (dark ? 0.34 : 0.3) * (l.chip ? 1.35 : 1) * k
      const P = pathOf(l, f, t)
      const lg = g.createLinearGradient(P[0].x, P[0].y, P[3].x, P[3].y)
      lg.addColorStop(0, color(l.ci, l.chip ? base * 0.7 : 0))
      lg.addColorStop(0.55, color(l.ci, base * 0.4))
      lg.addColorStop(1, color(l.ci, base))
      g.strokeStyle = lg
      g.lineWidth = l.w
      g.beginPath()
      g.moveTo(P[0].x, P[0].y)
      g.bezierCurveTo(P[1].x, P[1].y, P[2].x, P[2].y, P[3].x, P[3].y)
      g.stroke()

      for (const p of l.pulses) {
        const before = p.t
        p.t += p.v * dt
        if (l.chip && before <= 0 && p.t > 0) {
          l.chip.flash = 1
          l.chip.rip = 0
        }
        if (p.t >= 1) {
          p.t = rand(-0.9, -0.1)
          p.v = l.chip ? rand(0.12, 0.16) : rand(0.11, 0.2)
          flash = Math.min(1, flash + 0.22)
        }
        if (p.t <= 0) continue
        const u = Math.pow(p.t, 1.35)
        const N = 14
        const len = 0.085
        let prev = bezier(P, u)
        for (let s = 1; s <= N; s++) {
          const uu = u - (len * s) / N
          if (uu < 0) break
          const q = bezier(P, uu)
          const kk = 1 - s / N
          g.strokeStyle = color(l.ci, kk * (dark ? 0.9 : 0.75) * k)
          g.lineWidth = 0.5 + 1.9 * kk
          g.beginPath()
          g.moveTo(prev.x, prev.y)
          g.lineTo(q.x, q.y)
          g.stroke()
          prev = q
        }
        const head = bezier(P, u)
        g.fillStyle = color(l.ci, (dark ? 1 : 0.9) * k)
        g.beginPath()
        g.arc(head.x, head.y, 1.9, 0, TAU)
        g.fill()
      }
      if (l.chip) drawChip(l, t, dt)
    }
    flash *= Math.pow(0.15, dt)
  }

  function paint(dt: number) {
    g.globalCompositeOperation = 'source-over'
    g.clearRect(0, 0, W, H)
    draw(dt, T)
  }

  function frame(ts: number) {
    raf = 0
    if (destroyed) return
    const dt = last ? Math.min(0.05, (ts - last) / 1000) : 0
    last = ts
    T += dt
    paint(dt)
    if (!document.hidden) raf = requestAnimationFrame(frame)
  }

  function start() {
    if (destroyed) return
    if (motion) {
      if (!raf && !document.hidden) {
        last = 0
        raf = requestAnimationFrame(frame)
      }
    } else {
      if (!T) T = 14
      paint(0)
    }
  }

  function resize() {
    if (destroyed) return
    const dpr = Math.min(window.devicePixelRatio || 1, 2)
    const box = canvas.getBoundingClientRect()
    W = Math.round(box.width)
    H = Math.round(box.height)
    canvas.width = Math.round(W * dpr)
    canvas.height = Math.round(H * dpr)
    g.setTransform(dpr, 0, 0, dpr, 0, 0)
    initLines()
    if (!motion) start()
  }

  const onResize = () => {
    clearTimeout(resizeTimer)
    resizeTimer = setTimeout(resize, 120)
  }
  const onVisibility = () => {
    if (!document.hidden) start()
  }

  // 静止模式下切换深浅色需要重画一帧；动画模式每帧都会读取当前主题
  let themeObserver: MutationObserver | null = null
  if (!motion && typeof MutationObserver !== 'undefined') {
    themeObserver = new MutationObserver(() => {
      if (!destroyed) paint(0)
    })
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  }

  window.addEventListener('resize', onResize)
  document.addEventListener('visibilitychange', onVisibility)
  document.fonts?.ready?.then(() => {
    if (destroyed) return
    initLines()
    if (!motion) paint(0)
  }).catch(() => {})

  resize()
  start()

  return {
    refresh() {
      resize()
    },
    destroy() {
      destroyed = true
      if (raf) cancelAnimationFrame(raf)
      raf = 0
      clearTimeout(resizeTimer)
      window.removeEventListener('resize', onResize)
      document.removeEventListener('visibilitychange', onVisibility)
      themeObserver?.disconnect()
    }
  }
}
