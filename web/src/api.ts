// Вызовы JSON API backend'а X-tractor. Весь API - один POST /api/ с полем action;
// UI никогда не обращается к Elasticsearch/OpenSearch напрямую.

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
}

// SessionError - вместо JSON пришла HTML-страница: прокси авторизации перед сервисом
// отправил запрос на вход.
export class SessionError extends Error {}

// Backend отвечает ошибками через http.Error: обычный текст или текст с JSON вида
// {"error":"..."}. Достаём из него читаемое сообщение.
function errorText(text: string, status: number): string {
  const t = text.trim()
  try {
    const j = JSON.parse(t)
    if (j && typeof j.error === 'string') return j.error
  } catch {
    // не JSON - вернём текст как есть
  }
  return t || `HTTP ${status}`
}

async function call(action: string, body: Record<string, unknown> = {}): Promise<string> {
  let r: Response
  try {
    r = await fetch('/api/', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, ...body }),
    })
  } catch {
    throw new ApiError(0, 'сервер недоступен')
  }
  if ((r.headers.get('Content-Type') ?? '').includes('text/html')) {
    throw new SessionError('session lost')
  }
  const text = await r.text()
  if (!r.ok) throw new ApiError(r.status, errorText(text, r.status))
  return text
}

async function callJSON<T>(action: string, body: Record<string, unknown> = {}): Promise<T> {
  return JSON.parse(await call(action, body)) as T
}

/* ---------- Снапшоты ---------- */

export interface Repository {
  id: string
  type: string
}

export interface Node {
  name: string
  ip: string
  total: number
  used: number
  avail: number
  percent: number
}

export interface Snapshot {
  name: string
  uuid: string
  state?: string
  // Дата из имени снапшота (name-YYYY.MM.DD), полночь UTC
  date: number
}

export interface SnapshotIndex {
  name: string
  shards: number
  size: number
}

export interface Restored {
  name: string
  // Исходный индекс и снапшот - из источника восстановления, если ES их ещё помнит
  source?: string
  snapshot?: string
  repository?: string
  total: number
  recovered: number
  percent: number
  done: boolean
  started?: number
}

export interface RestoreResult {
  restoring: string[]
  // Индексы, для которых не хватило места на узлах (решает backend, функция Barrel)
  skipped: string[]
}

export function getRepositories(): Promise<Repository[]> {
  return callJSON<Repository[]>('get_repositories')
}

interface rawNode {
  ip: string
  name: string
  du: string
  dup: string
  d: string
}

// get_nodes отдаёт _cat/nodes в байтах: du - занято, d - свободно, dup - процент.
export async function getNodes(): Promise<Node[]> {
  const raw = await callJSON<rawNode[]>('get_nodes')
  return raw.map((n) => {
    const used = Number(n.du) || 0
    const avail = Number(n.d) || 0
    return { name: n.name, ip: n.ip, used, avail, total: used + avail, percent: Number(n.dup) || 0 }
  })
}

interface rawSnapshot {
  snapshot: string
  uuid: string
  state?: string
  CreateEpoch: number
}

export async function getSnapshots(repo: string): Promise<Snapshot[]> {
  const raw = await callJSON<rawSnapshot[] | null>('get_snapshots', { values: { repo, otype: 'time', odir: 'desc' } })
  return (raw ?? []).map((s) => ({ name: s.snapshot, uuid: s.uuid, state: s.state, date: s.CreateEpoch * 1000 }))
}

interface rawStatus {
  snapshots?: {
    indices?: Record<string, { shards_stats?: { total?: number }; stats?: { total?: { size_in_bytes?: number } } }>
  }[]
}

export async function getSnapshotIndices(repo: string, snapshot: string): Promise<SnapshotIndex[]> {
  const raw = await callJSON<rawStatus>('get_snapshot', { values: { repo, snapshot } })
  const indices = raw.snapshots?.[0]?.indices ?? {}
  return Object.entries(indices)
    .map(([name, i]) => ({ name, shards: i.shards_stats?.total ?? 0, size: i.stats?.total?.size_in_bytes ?? 0 }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

interface rawRecoveryShard {
  stage?: string
  start_time_in_millis?: number
  source?: { repository?: string; snapshot?: string; index?: string }
  index?: { size?: { total_in_bytes?: number; recovered_in_bytes?: number } }
}

// get_indices - это _recovery индексов extracted*: по шардам складываем общий прогресс.
export async function getRestored(): Promise<Restored[]> {
  const raw = await callJSON<Record<string, { shards?: rawRecoveryShard[] }>>('get_indices')
  return Object.entries(raw)
    .map(([name, r]) => {
      const shards = r.shards ?? []
      let total = 0
      let recovered = 0
      let started: number | undefined
      let done = shards.length > 0
      const src = shards.find((s) => s.source?.snapshot)?.source
      for (const s of shards) {
        total += s.index?.size?.total_in_bytes ?? 0
        recovered += s.index?.size?.recovered_in_bytes ?? 0
        if (s.stage !== 'DONE') done = false
        const t = s.start_time_in_millis
        if (t && (started === undefined || t < started)) started = t
      }
      const percent = done ? 100 : total > 0 ? Math.min(99, Math.floor((recovered / total) * 100)) : 0
      return {
        name,
        source: src?.index,
        snapshot: src?.snapshot,
        repository: src?.repository,
        total,
        recovered: done ? total : recovered,
        percent,
        done,
        started,
      }
    })
    .sort((a, b) => (b.started ?? 0) - (a.started ?? 0))
}

export async function deleteIndex(index: string): Promise<void> {
  await call('del_index', { values: { index } })
}

// restore: что восстанавливается и что пропущено из-за нехватки места на узлах.
// Если места не хватило ни одному индексу, кластер не вызывается, restoring пуст.
export async function restore(repo: string, snapshot: string, indices: string[]): Promise<RestoreResult> {
  const r = await callJSON<Partial<RestoreResult>>('restore', { values: { repo, snapshot, indices } })
  return { restoring: r.restoring ?? [], skipped: r.skipped ?? [] }
}

/* ---------- Поиск ---------- */

export interface Cluster {
  name: string
  host: string
  // Тип кластера - его и ждёт backend в поле search.cluster
  type: 'Snapshot' | 'Search'
}

export type Operation = 'is' | 'is_not' | 'exists' | 'does_not_exists'

export interface Filter {
  field: string
  operation: Operation
  value: string
}

export interface SearchParams {
  cluster: string
  index: string
  xql: string
  fields: string[]
  mapping: string[]
  filters: Record<string, Filter>
  timefields: string[]
  // Время в UTC: "YYYY-MM-DD HH:mm:ss"
  date_start: string
  date_end: string
}

export interface Hit {
  _id?: string
  _index?: string
  _source?: Record<string, unknown>
  fields?: Record<string, unknown[]>
}

export interface SearchResult {
  hits: Hit[]
  took?: number
  // Продолжение выдачи для следующей страницы; нет - страниц больше нет
  next?: string
}

export async function getClusters(): Promise<Cluster[]> {
  const raw = await callJSON<{ Name: string; Host: string; Type: 'Snapshot' | 'Search' }[]>('get_clusters')
  return raw.map((c) => ({ name: c.Name, host: c.Host, type: c.Type }))
}

export async function getIndexGroups(cluster: string): Promise<string[]> {
  const raw = await callJSON<{ index: string }[] | null>('get_index_groups', { search: { cluster } })
  return (raw ?? []).map((g) => g.index).sort()
}

// get_mapping - плоская карта "поле -> тип" по сегодняшнему индексу группы.
export function getMapping(cluster: string, index: string): Promise<Record<string, string>> {
  return callJSON<Record<string, string>>('get_mapping', { search: { cluster, index } })
}

// search отдаёт страницу до 500 записей; следующую - по next из предыдущего ответа.
export async function search(p: SearchParams, after?: string): Promise<SearchResult> {
  const raw = await callJSON<{ took?: number; hits?: { hits?: Hit[] }; next?: string }>('search', {
    search: { ...p, count: false, search_after: after ?? '' },
  })
  return { hits: raw.hits?.hits ?? [], took: raw.took, next: raw.next || undefined }
}

export async function count(p: SearchParams): Promise<number> {
  const raw = await callJSON<{ count?: number }>('search', { search: { ...p, count: true } })
  return raw.count ?? 0
}

// Гистограмма найденного по времени: время в мс UTC, интервал выбирает сервер (~100 столбцов).
export interface Histogram {
  from: number
  to: number
  interval: number
  total: number
  buckets: { t: number; n: number }[]
}

export function histogram(p: SearchParams): Promise<Histogram> {
  return callJSON<Histogram>('histogram', { search: { ...p, count: false } })
}

export type ExportFormat = 'csv' | 'json'

export interface ExportResult {
  file: string
  format: ExportFormat
  rows: number
  size: number
  // Выгрузку остановил лимит строк или размера файла
  truncated: boolean
}

// prepare_csv/prepare_json пишут файл /data/<fname>.<format> и описывают его.
export function exportFile(format: ExportFormat, p: SearchParams, fname: string): Promise<ExportResult> {
  return callJSON<ExportResult>(`prepare_${format}`, { search: { ...p, fname } })
}

export function exportURL(fname: string, format: ExportFormat): string {
  return `/data/${fname}.${format}`
}

// Имя файла выгрузки: backend принимает только [A-Za-z0-9_-].
export function newFname(): string {
  return Math.random().toString(36).slice(2, 10)
}

/* ---------- Прочее ---------- */

export interface Config {
  kibana: string
  version: string
  limits: { rows: number; size: number }
}

export function getConfig(): Promise<Config> {
  return callJSON<Config>('get_config')
}

// Адрес Kibana backend подставляет в <meta> страницы. Без схемы - считаем https,
// как старый интерфейс.
export function kibanaURL(): string | undefined {
  const v = document.querySelector<HTMLMetaElement>('meta[name="xtractor-kibana"]')?.content.trim()
  if (!v || v.includes('{{')) return undefined
  return /^https?:\/\//.test(v) ? v.replace(/\/$/, '') : `https://${v.replace(/\/$/, '')}`
}
