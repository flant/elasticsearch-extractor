// Форматирование чисел, размеров и дат для интерфейса на русском.

const nf = new Intl.NumberFormat('ru-RU')
const nf1 = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 })
const nf2 = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 2 })

export function num(n: number): string {
  return nf.format(n)
}

const UNITS = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ', 'ПБ']

export function bytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 Б'
  let i = 0
  let v = n
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024
    i++
  }
  const f = v >= 100 || i === 0 ? nf : v >= 10 ? nf1 : nf2
  return `${f.format(v)} ${UNITS[i]}`
}

const dayFmt = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'short', weekday: 'short', timeZone: 'UTC' })
const dateTimeFmt = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
const shortDayFmt = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'short' })

// "11 окт, сб" - день в UTC: даты в именах снапшотов и индексов без часового пояса.
export function day(ms: number): string {
  const parts = dayFmt.formatToParts(new Date(ms))
  const d = parts.find((p) => p.type === 'day')?.value
  const m = parts.find((p) => p.type === 'month')?.value?.replace('.', '')
  const w = parts.find((p) => p.type === 'weekday')?.value?.replace('.', '')
  return `${d} ${m}, ${w}`
}

export function shortDay(ms: number): string {
  return shortDayFmt.format(new Date(ms)).replace('.', '')
}

// Местное время пользователя: когда начали восстанавливать индекс.
export function dateTime(ms: number): string {
  return dateTimeFmt.format(new Date(ms)).replace('.', '')
}

export function percent(n: number): string {
  return `${nf1.format(n)}%`
}

export function seconds(ms: number): string {
  return `${nf1.format(ms / 1000)} с`
}

/* ---------- Время для поиска: строки UTC ---------- */

const pad = (n: number) => String(n).padStart(2, '0')

// "YYYY-MM-DD HH:mm:ss" в UTC - формат, который ждёт backend.
export function utcString(ms: number): string {
  const d = new Date(ms)
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`
}

// Значение <input type="datetime-local"> трактуем как UTC.
export function inputToUTC(v: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(v)
  if (!m) return null
  return Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +(m[6] ?? 0))
}

export function utcToInput(ms: number): string {
  return utcString(ms).replace(' ', 'T')
}

// Время документа из ES: ISO-строка, число или массив из fields.
export function docTime(v: unknown): string {
  const x = Array.isArray(v) ? v[0] : v
  if (typeof x === 'string') {
    const t = Date.parse(x)
    if (!Number.isNaN(t)) {
      const d = new Date(t)
      return `${utcString(t)}.${String(d.getUTCMilliseconds()).padStart(3, '0')}`
    }
    return x
  }
  if (typeof x === 'number') return utcString(x)
  return x == null ? '' : String(x)
}

/* ---------- Значения документов ---------- */

// Поле из документа: сначала fields (плоские имена), иначе _source по пути с точками.
export function fieldValue(doc: { _source?: Record<string, unknown>; fields?: Record<string, unknown[]> }, field: string): unknown {
  if (doc.fields && field in doc.fields) return doc.fields[field]
  const src = doc._source
  if (!src) return undefined
  if (field in src) return src[field]
  let cur: unknown = src
  for (const part of field.split('.')) {
    if (cur && typeof cur === 'object' && part in (cur as Record<string, unknown>)) cur = (cur as Record<string, unknown>)[part]
    else return undefined
  }
  return cur
}

export function display(v: unknown): string {
  if (v == null) return ''
  if (Array.isArray(v)) return v.map(display).join(', ')
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

// Плоский список "поле: значение" для раскрытого документа.
export function flatten(obj: unknown, prefix = '', out: [string, string][] = []): [string, string][] {
  if (obj && typeof obj === 'object' && !Array.isArray(obj)) {
    for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
      flatten(v, prefix ? `${prefix}.${k}` : k, out)
    }
  } else {
    out.push([prefix, display(obj)])
  }
  return out
}

/* ---------- Подсветка найденного ---------- */

// Слова и фразы из строки поиска simple_query_string, кроме исключённых (-слово)
// и операторов. Подсветка приблизительная: анализатор ES на клиенте не повторить.
export function searchTerms(q: string): string[] {
  const terms: string[] = []
  for (const m of q.matchAll(/(-?)"([^"]+)"|(\S+)/g)) {
    if (m[2] !== undefined) {
      if (!m[1]) terms.push(m[2])
      continue
    }
    let w = m[3]
    if (w.startsWith('-') || ['AND', 'OR', 'NOT', '|', '+'].includes(w)) continue
    w = w.replace(/^[+(]+|[)~*]+$/g, '').replace(/\*$/, '')
    if (w.length > 1) terms.push(w)
  }
  return terms
}

export interface Segment {
  text: string
  hit: boolean
}

export function highlight(text: string, terms: string[]): Segment[] {
  if (!terms.length || !text) return [{ text, hit: false }]
  const re = new RegExp(terms.map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|'), 'gi')
  const out: Segment[] = []
  let last = 0
  for (const m of text.matchAll(re)) {
    if (m.index === undefined || m[0] === '') continue
    if (m.index > last) out.push({ text: text.slice(last, m.index), hit: false })
    out.push({ text: m[0], hit: true })
    last = m.index + m[0].length
  }
  if (last < text.length) out.push({ text: text.slice(last), hit: false })
  return out
}
