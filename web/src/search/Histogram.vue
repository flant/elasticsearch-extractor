<script setup lang="ts">
// Гистограмма найденного по времени, как в Discover у OpenSearch Dashboards.
// Только два действия: клик по столбцу или выделение области мышкой. Выбранный промежуток
// уходит наверх (select): страница ставит его в поля времени и сразу ищет по нему.
// Рисуется на canvas без библиотек, перерисовка - не чаще раза за кадр.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Histogram } from '../api'

const props = defineProps<{ data: Histogram | null; loading?: boolean }>()
const emit = defineEmits<{ select: [from: number, to: number] }>()

const SEC = 1000
const MIN = 60 * SEC
const HOUR = 60 * MIN
const DAY = 24 * HOUR
// Шаги подписей оси времени: подпись не чаще, чем раз в ~90px
const STEPS = [1, 2, 5, 10, 15, 30]
  .map((s) => s * SEC)
  .concat([1, 2, 5, 10, 15, 30].map((m) => m * MIN), [1, 2, 3, 6, 12].map((h) => h * HOUR), [DAY, 2 * DAY, 7 * DAY])
const PAD = { l: 44, r: 8, t: 8, b: 20 }

const box = ref<HTMLElement>()
// Подсказка над столбцом: число записей и его промежуток
const tip = ref<{ left: number; count: string; period: string } | null>(null)
const tipEl = ref<HTMLElement>()
const canvas = ref<HTMLCanvasElement>()
const nf = new Intl.NumberFormat('ru-RU')
let drag: { x0: number; x1: number } | null = null
// Нарисованные столбцы (в CSS-пикселях): по ним видно, что курсор над столбцом
let rects: { x0: number; x1: number; y0: number; t: number; n: number }[] = []
let baseline = 0

/* ---------- Отрисовка ---------- */

let frame = 0
function schedule() {
  if (!frame) frame = requestAnimationFrame(() => ((frame = 0), draw()))
}

const pad = (n: number) => String(n).padStart(2, '0')

function label(t: number, step: number): string {
  const d = new Date(t)
  if (step < MIN) return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`
  if (step < DAY && (d.getUTCHours() !== 0 || d.getUTCMinutes() !== 0)) return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`
  return `${pad(d.getUTCDate())}.${pad(d.getUTCMonth() + 1)}`
}

// Время с секундами; дата - если промежуток длиннее суток
function edge(t: number, span: number): string {
  const d = new Date(t)
  const time = `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`
  return span >= DAY ? `${pad(d.getUTCDate())}.${pad(d.getUTCMonth() + 1)} ${time}` : time
}

function human(ms: number): string {
  if (ms < MIN) return `${ms / SEC} с`
  if (ms < HOUR) return `${ms / MIN} мин`
  if (ms < DAY) return `${ms / HOUR} ч`
  return `${ms / DAY} дн`
}

function width(): number {
  return box.value?.clientWidth ?? 0
}

function x(t: number, w: number): number {
  const h = props.data!
  return PAD.l + ((t - h.from) / (h.to - h.from)) * (w - PAD.l - PAD.r)
}

function tAt(px: number, w: number): number {
  const h = props.data!
  return h.from + ((px - PAD.l) / (w - PAD.l - PAD.r)) * (h.to - h.from)
}

function draw() {
  const c = canvas.value
  const h = props.data
  if (!c || !box.value) return
  const ctx = c.getContext('2d')
  if (!ctx) return
  const dpr = window.devicePixelRatio || 1
  const w = box.value.clientWidth
  const hh = box.value.clientHeight
  if (c.width !== Math.round(w * dpr) || c.height !== Math.round(hh * dpr)) {
    c.width = Math.round(w * dpr)
    c.height = Math.round(hh * dpr)
  }
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
  ctx.clearRect(0, 0, w, hh)
  if (!h || h.to <= h.from) return

  const css = getComputedStyle(document.documentElement)
  const bar = css.getPropertyValue('--bs-primary').trim()
  const grid = css.getPropertyValue('--bs-border-color').trim()
  const text = css.getPropertyValue('--bs-secondary-color').trim()
  ctx.font = '11px ' + css.getPropertyValue('--bs-body-font-family')

  // Ось Y: число записей, поэтому шаг делений целый и не меньше 1 - дробных записей нет
  const max = Math.max(1, ...h.buckets.map((b) => b.n))
  const mag = Math.max(1, 10 ** Math.floor(Math.log10(max / 3)))
  const yStep = [1, 2, 5, 10].map((k) => k * mag).find((s) => max / s <= 4) ?? 10 * mag
  const top = Math.ceil(max / yStep) * yStep
  const y = (n: number) => hh - PAD.b - (n / top) * (hh - PAD.t - PAD.b)
  ctx.strokeStyle = grid
  ctx.fillStyle = text
  ctx.lineWidth = 1
  ctx.textAlign = 'right'
  ctx.textBaseline = 'middle'
  for (let v = 0; v <= top; v += yStep) {
    const yy = Math.round(y(v)) + 0.5
    ctx.beginPath()
    ctx.moveTo(PAD.l, yy)
    ctx.lineTo(w - PAD.r, yy)
    ctx.stroke()
    ctx.fillText(nf.format(v), PAD.l - 6, yy)
  }

  // Столбцы; крайние обрезаются границами промежутка
  const bw = Math.max(1, x(h.from + h.interval, w) - x(h.from, w) - 1)
  ctx.fillStyle = bar
  ctx.globalAlpha = 0.8
  rects = []
  baseline = y(0)
  for (const b of h.buckets) {
    const x0 = Math.max(PAD.l, x(b.t, w))
    const x1 = Math.min(w - PAD.r, x(b.t, w) + bw)
    if (x1 > x0 && b.n > 0) {
      ctx.fillRect(x0, y(b.n), x1 - x0, baseline - y(b.n))
      rects.push({ x0, x1, y0: y(b.n), t: b.t, n: b.n })
    }
  }
  ctx.globalAlpha = 1

  // Ось времени. Начало и конец промежутка подписаны всегда, с секундами: при сильном
  // увеличении промежуточных меток может не остаться, а период должен быть виден.
  const span = h.to - h.from
  const ty = hh - PAD.b + 5
  ctx.fillStyle = text
  ctx.textBaseline = 'top'
  const left = edge(h.from, span)
  const right = edge(h.to, span)
  const lw = ctx.measureText(left).width
  const rw = ctx.measureText(right).width
  ctx.textAlign = 'left'
  ctx.fillText(left, PAD.l, ty)
  if (lw + rw + 16 <= w - PAD.l - PAD.r) {
    ctx.textAlign = 'right'
    ctx.fillText(right, w - PAD.r, ty)
  }
  // Промежуточные метки - только там, где они не наезжают на крайние
  const tStep = STEPS.find((s) => (s / span) * (w - PAD.l - PAD.r) >= 90) ?? STEPS[STEPS.length - 1]
  ctx.textAlign = 'center'
  for (let t = Math.ceil(h.from / tStep) * tStep; t <= h.to; t += tStep) {
    const xx = x(t, w)
    const s = label(t, tStep)
    const half = ctx.measureText(s).width / 2
    if (xx - half < PAD.l + lw + 8 || xx + half > w - PAD.r - rw - 8) continue
    ctx.fillText(s, xx, ty)
  }

  // Выделяемая область
  if (drag && Math.abs(drag.x1 - drag.x0) > 2) {
    const a = Math.max(PAD.l, Math.min(drag.x0, drag.x1))
    const b = Math.min(w - PAD.r, Math.max(drag.x0, drag.x1))
    ctx.fillStyle = bar
    ctx.globalAlpha = 0.15
    ctx.fillRect(a, PAD.t, b - a, hh - PAD.t - PAD.b)
    ctx.globalAlpha = 1
    ctx.strokeStyle = bar
    ctx.strokeRect(a + 0.5, PAD.t + 0.5, b - a - 1, hh - PAD.t - PAD.b - 1)
  }
}

/* ---------- Мышь: клик по столбцу или выделение области ---------- */

function local(e: PointerEvent): number {
  return e.clientX - canvas.value!.getBoundingClientRect().left
}

// Над столбцом курсор - "палец": по столбцу можно кликнуть. В остальном месте - крестик
// для выделения области (он задан в CSS).
function pointer(e: PointerEvent) {
  const c = canvas.value!
  const r = c.getBoundingClientRect()
  const px = e.clientX - r.left
  const py = e.clientY - r.top
  const bar = props.loading || py > baseline ? undefined : rects.find((b) => px >= b.x0 && px <= b.x1 && py >= b.y0)
  c.style.cursor = bar ? 'pointer' : ''
  showTip(bar, px)
}

function showTip(bar: (typeof rects)[number] | undefined, px: number) {
  const h = props.data
  if (!bar || !h) {
    tip.value = null
    return
  }
  // промежуток столбца, обрезанный границами поиска; дата - если он длиннее суток или через полночь
  const a = Math.max(h.from, bar.t)
  const b = Math.min(h.to, bar.t + h.interval)
  const days = h.interval >= DAY || new Date(a).getUTCDate() !== new Date(b).getUTCDate() ? DAY : 0
  tip.value = { left: px, count: nf.format(bar.n), period: `${edge(a, days)} - ${edge(b, days)}` }
  // не выходить за правый край
  requestAnimationFrame(() => {
    const el = tipEl.value
    if (el && tip.value) tip.value.left = Math.max(0, Math.min(px + 12, width() - el.offsetWidth))
  })
}

// Промежуток наверх: в поля времени попадают целые секунды, не короче секунды
function select(a: number, b: number) {
  const h = props.data!
  a = Math.max(h.from, Math.floor(a / SEC) * SEC)
  b = Math.min(h.to, Math.ceil(b / SEC) * SEC)
  if (b - a < SEC) b = a + SEC
  emit('select', a, b)
}

function down(e: PointerEvent) {
  if (e.button !== 0 || !props.data || props.loading) return
  canvas.value!.setPointerCapture(e.pointerId)
  canvas.value!.style.cursor = ''
  tip.value = null
  drag = { x0: local(e), x1: local(e) }
}

function move(e: PointerEvent) {
  if (!drag) {
    pointer(e)
    return
  }
  drag.x1 = local(e)
  schedule()
}

function leave() {
  tip.value = null
  if (!drag && canvas.value) canvas.value.style.cursor = ''
}

function up() {
  if (!drag || !props.data) return
  const d = drag
  const w = width()
  drag = null
  schedule()
  if (Math.abs(d.x1 - d.x0) > 4) {
    // выделение области
    select(tAt(Math.min(d.x0, d.x1), w), tAt(Math.max(d.x0, d.x1), w))
    return
  }
  // клик по столбцу: его промежуток, обрезанный границами гистограммы
  const t = tAt(d.x0, w)
  const b = props.data.buckets.find((b) => t >= b.t && t < b.t + props.data!.interval)
  if (b) select(b.t, b.t + props.data.interval)
}

function cancel() {
  drag = null
  schedule()
}

watch(
  () => props.data,
  () => {
    tip.value = null
    schedule()
  },
)

let resize: ResizeObserver | undefined
let theme: MutationObserver | undefined
onMounted(() => {
  resize = new ResizeObserver(schedule)
  if (box.value) resize.observe(box.value)
  // цвета берутся из переменных Bootstrap: при смене темы - перерисовать
  theme = new MutationObserver(schedule)
  theme.observe(document.documentElement, { attributes: true, attributeFilter: ['data-bs-theme'] })
  schedule()
})
onBeforeUnmount(() => {
  resize?.disconnect()
  theme?.disconnect()
  cancelAnimationFrame(frame)
})
</script>

<template>
  <div class="card">
    <div class="card-body py-2">
      <div class="d-flex flex-wrap align-items-center gap-2 small mb-1">
        <span v-if="data" class="text-body-secondary">Интервал: <strong class="text-body">{{ human(data.interval) }}</strong> (авто)</span>
        <span class="text-body-secondary d-none d-md-inline">· кликните столбец или выделите область, чтобы искать в этом промежутке</span>
        <span v-if="loading" class="ms-auto text-body-secondary"><span class="spinner-border spinner-border-sm" aria-hidden="true"></span></span>
      </div>
      <div ref="box" class="chart" :class="{ dim: loading }">
        <canvas
          ref="canvas"
          role="img"
          aria-label="Число найденных записей по времени"
          @pointerdown="down"
          @pointermove="move"
          @pointerup="up"
          @pointercancel="cancel"
          @pointerleave="leave"
        ></canvas>
        <div v-if="tip" ref="tipEl" class="chart-tip small" :style="{ left: `${tip.left}px` }">
          <strong class="tabular">{{ tip.count }}</strong>
          <div class="tabular text-body-secondary">{{ tip.period }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.chart-tip {
  position: absolute;
  top: 4px;
  z-index: 2;
  pointer-events: none;
  white-space: nowrap;
  padding: 2px 8px;
  border-radius: var(--bs-border-radius-sm);
  background: var(--bs-body-bg);
  border: 1px solid var(--bs-border-color);
  box-shadow: var(--bs-box-shadow-sm);
}

.chart {
  position: relative;
  height: 170px;
  user-select: none;
  touch-action: none;
  transition: opacity 0.15s;
}

.chart.dim {
  opacity: 0.5;
}

canvas {
  display: block;
  width: 100%;
  height: 100%;
  cursor: crosshair;
}
</style>
