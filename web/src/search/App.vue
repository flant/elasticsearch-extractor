<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  SessionError,
  count,
  exportFile,
  exportURL,
  getClusters,
  getConfig,
  getIndexGroups,
  getMapping,
  newFname,
  search,
  type Cluster,
  type ExportFormat,
  type ExportResult,
  type Filter,
  type Hit,
  type SearchParams,
} from '../api'
import { bytes, inputToUTC, num, searchTerms, seconds, utcString, utcToInput } from '../format'
import { errorMessage } from '../ui'
import Icon from '../components/Icon.vue'
import TopBar from '../components/TopBar.vue'
import FieldList from './FieldList.vue'
import FilterDialog from './FilterDialog.vue'
import ResultsTable from './ResultsTable.vue'

const MIN = 60_000
const PRESETS = [
  { id: '15m', title: '15 мин', ms: 15 * MIN },
  { id: '2h', title: '2 ч', ms: 120 * MIN },
  { id: '24h', title: '24 ч', ms: 1440 * MIN },
  { id: '7d', title: '7 дн', ms: 7 * 1440 * MIN },
]

const clusters = ref<Cluster[]>([])
const cluster = ref('')
const groups = ref<string[]>([])
const group = ref('')
const mapping = ref<Record<string, string>>({})
const fields = ref<string[]>([])
const timefield = ref('')
const sourceError = ref('')
const sourceLoading = ref(false)

const xql = ref('')
const from = ref('')
const to = ref('')
const preset = ref<string | null>('15m')
const filters = ref<Record<string, Filter>>({})
const editing = ref<{ id: string | null; filter?: Filter } | null>(null)

const hits = ref<Hit[]>([])
// Продолжение выдачи с сервера: следующая страница по прокрутке
const next = ref<string | undefined>()
const loadingMore = ref(false)
const total = ref<number | null>(null)
const took = ref<number | undefined>()
const searched = ref<SearchParams | null>(null)
const searching = ref(false)
const searchError = ref('')
const sessionLost = ref(false)

const exporting = ref<{
  fname: string
  format: ExportFormat
  phase: 'running' | 'done' | 'error'
  result?: ExportResult
  error?: string
} | null>(null)
const limits = ref<{ rows: number; size: number } | null>(null)

const current = computed(() => clusters.value.find((c) => c.type === cluster.value))
const dateFields = computed(() =>
  Object.entries(mapping.value)
    .filter(([, t]) => t === 'date' || t === 'date_nanos')
    .map(([n]) => n)
    .sort(),
)
const columns = computed(() => fields.value.filter((f) => f !== timefield.value))
const terms = computed(() => searchTerms(searched.value?.xql ?? ''))
const filterList = computed(() => Object.entries(filters.value))
const fileName = computed(() => (exporting.value ? `${exporting.value.fname}.${exporting.value.format}` : ''))

function setPreset(id: string) {
  const p = PRESETS.find((x) => x.id === id)
  if (!p) return
  const now = Date.now()
  preset.value = id
  from.value = utcToInput(now - p.ms)
  to.value = utcToInput(now)
}

function fail(e: unknown, target: { value: string }) {
  if (e instanceof SessionError) sessionLost.value = true
  target.value = errorMessage(e)
}

/* ---------- Источник: кластер, индексы, поля ---------- */

async function loadGroups() {
  groups.value = []
  sourceError.value = ''
  if (!cluster.value) return
  sourceLoading.value = true
  try {
    groups.value = await getIndexGroups(cluster.value)
  } catch (e) {
    fail(e, sourceError)
  } finally {
    sourceLoading.value = false
  }
}

async function loadMapping() {
  mapping.value = {}
  if (!cluster.value || !group.value) return
  sourceError.value = ''
  try {
    mapping.value = await getMapping(cluster.value, group.value)
  } catch (e) {
    fail(e, sourceError)
  }
  // Поле времени: @timestamp, если он есть, иначе первое поле с датой.
  timefield.value = dateFields.value.includes('@timestamp') ? '@timestamp' : (dateFields.value[0] ?? '')
  fields.value = fields.value.filter((f) => f in mapping.value)
}

async function selectCluster(type: string, keepGroup = '') {
  cluster.value = type
  group.value = ''
  await loadGroups()
  group.value = groups.value.includes(keepGroup) ? keepGroup : (groups.value[0] ?? '')
}

watch(group, () => loadMapping())

/* ---------- Поиск ---------- */

function params(): SearchParams | null {
  const ds = inputToUTC(from.value)
  const de = inputToUTC(to.value)
  if (timefield.value && (ds === null || de === null)) {
    searchError.value = 'Задайте начало и конец промежутка'
    return null
  }
  if (ds !== null && de !== null && de <= ds) {
    searchError.value = 'Конец промежутка должен быть позже начала'
    return null
  }
  return {
    cluster: cluster.value,
    index: group.value,
    xql: xql.value.trim(),
    fields: columns.value,
    mapping: Object.keys(mapping.value),
    filters: { ...filters.value },
    timefields: timefield.value ? [timefield.value] : [],
    date_start: ds !== null ? utcString(ds) : '',
    date_end: de !== null ? utcString(de) : '',
  }
}

async function run() {
  if (!cluster.value || !group.value || searching.value) return
  // Быстрый выбор отсчитывается от момента поиска.
  if (preset.value) setPreset(preset.value)
  searchError.value = ''
  const p = params()
  if (!p) return
  searching.value = true
  writeURL()
  try {
    const [res, n] = await Promise.all([search(p), count(p).catch(() => null)])
    hits.value = res.hits
    next.value = res.next
    took.value = res.took
    total.value = n
    searched.value = p
  } catch (e) {
    fail(e, searchError)
    hits.value = []
    next.value = undefined
    total.value = null
    searched.value = null
  } finally {
    searching.value = false
  }
}

// Следующая страница с сервера: продолжает выдачу последнего поиска.
async function loadMore() {
  if (!searched.value || !next.value || loadingMore.value) return
  const p = searched.value
  loadingMore.value = true
  try {
    const res = await search(p, next.value)
    if (searched.value !== p) return
    hits.value = [...hits.value, ...res.hits]
    next.value = res.next
  } catch (e) {
    fail(e, searchError)
    next.value = undefined
  } finally {
    loadingMore.value = false
  }
}

/* ---------- Фильтры ---------- */

let seq = 0

function saveFilter(f: Filter) {
  const id = editing.value?.id ?? `f${Date.now().toString(36)}${seq++}`
  filters.value = { ...filters.value, [id]: f }
  editing.value = null
}

function removeFilter(id: string) {
  const next = { ...filters.value }
  delete next[id]
  filters.value = next
  if (editing.value?.id === id) editing.value = null
}

function filterLabel(f: Filter): { not: boolean; field: string; rest: string } {
  switch (f.operation) {
    case 'is':
      return { not: false, field: f.field, rest: `: ${f.value}` }
    case 'is_not':
      return { not: true, field: f.field, rest: `: ${f.value}` }
    case 'exists':
      return { not: false, field: f.field, rest: ' существует' }
    default:
      return { not: true, field: f.field, rest: ' не существует' }
  }
}

/* ---------- Выгрузка ---------- */

async function doExport(format: ExportFormat) {
  if (!searched.value || exporting.value?.phase === 'running') return
  const fname = newFname()
  exporting.value = { fname, format, phase: 'running' }
  try {
    // Выгружаем ровно то, что показано: параметры последнего поиска
    const result = await exportFile(format, searched.value, fname)
    exporting.value = { fname, format, phase: 'done', result }
    const a = document.createElement('a')
    a.href = exportURL(fname, format)
    a.download = `${fname}.${format}`
    a.click()
  } catch (e) {
    if (e instanceof SessionError) sessionLost.value = true
    exporting.value = { fname, format, phase: 'error', error: errorMessage(e) }
  }
}

/* ---------- Состояние в адресе страницы ---------- */

function writeURL() {
  const u = new URL(window.location.href)
  const q = u.searchParams
  q.set('cluster', cluster.value)
  q.set('index', group.value)
  const set = (k: string, v: string) => (v ? q.set(k, v) : q.delete(k))
  set('q', xql.value.trim())
  set('fields', columns.value.join(','))
  set('filters', filterList.value.length ? JSON.stringify(Object.values(filters.value)) : '')
  if (preset.value) {
    q.set('range', preset.value)
    q.delete('from')
    q.delete('to')
  } else {
    q.delete('range')
    set('from', from.value)
    set('to', to.value)
  }
  history.replaceState(null, '', u)
}

function readURL(): { cluster?: string; index?: string; fields: string[] } {
  const q = new URL(window.location.href).searchParams
  xql.value = q.get('q') ?? ''
  const range = q.get('range')
  if (q.get('from') && q.get('to') && !range) {
    preset.value = null
    from.value = q.get('from')!
    to.value = q.get('to')!
  } else setPreset(PRESETS.some((p) => p.id === range) ? range! : '15m')
  try {
    const list = JSON.parse(q.get('filters') ?? '[]') as Filter[]
    const next: Record<string, Filter> = {}
    list.forEach((f, i) => {
      if (f && typeof f.field === 'string') next[`u${i}`] = f
    })
    filters.value = next
  } catch {
    filters.value = {}
  }
  return {
    cluster: q.get('cluster') ?? undefined,
    index: q.get('index') ?? undefined,
    fields: (q.get('fields') ?? '').split(',').filter(Boolean),
  }
}

async function refresh() {
  await loadGroups()
  if (!groups.value.includes(group.value)) group.value = groups.value[0] ?? ''
  else await loadMapping()
}

function reload() {
  window.location.reload()
}

onMounted(async () => {
  const url = readURL()
  getConfig()
    .then((c) => (limits.value = c.limits))
    .catch(() => {})
  try {
    clusters.value = await getClusters()
  } catch (e) {
    fail(e, sourceError)
    return
  }
  const wanted = clusters.value.find((c) => c.type === url.cluster) ?? clusters.value[0]
  if (!wanted) return
  await selectCluster(wanted.type, url.index ?? '')
  fields.value = url.fields
})
</script>

<template>
  <TopBar page="search" :cluster="current?.name" :host="current?.host" :loading="sourceLoading" @refresh="refresh" />
  <div v-if="sessionLost" class="alert alert-danger d-flex align-items-center gap-2 m-3 mb-0">
    <Icon name="alert" /> Сессия входа истекла или сервер недоступен.
    <button class="btn btn-sm btn-outline-danger ms-auto" @click="reload">Обновить страницу</button>
  </div>

  <div class="container-fluid py-3">
    <div class="row g-3">
      <!-- Слева: источник и поля -->
      <aside class="col-12 col-md-4 col-xl-3">
        <div class="card">
          <div class="card-body d-flex flex-column gap-3">
            <div>
              <div class="section-title mb-2">Кластер</div>
              <div class="btn-group btn-group-sm w-100" role="group" aria-label="Кластер">
                <button
                  v-for="c in clusters"
                  :key="c.type"
                  type="button"
                  class="btn btn-outline-primary"
                  :class="{ active: c.type === cluster }"
                  :aria-pressed="c.type === cluster"
                  :title="c.host"
                  @click="selectCluster(c.type)"
                >
                  {{ c.name || c.type }}
                </button>
              </div>
            </div>

            <div>
              <label class="section-title mb-2" for="igs">Индексы</label>
              <select id="igs" v-model="group" class="form-select form-select-sm" :disabled="!groups.length">
                <option v-if="!groups.length" value="">{{ sourceLoading ? 'Загружаем…' : 'Нет индексов за сегодня' }}</option>
                <option v-for="g in groups" :key="g" :value="g">{{ g }}</option>
              </select>
              <template v-if="dateFields.length > 1">
                <label class="section-title mt-3 mb-2" for="tf">Поле времени</label>
                <select id="tf" v-model="timefield" class="form-select form-select-sm font-monospace">
                  <option v-for="d in dateFields" :key="d" :value="d">{{ d }}</option>
                </select>
              </template>
              <div v-if="sourceError" class="alert alert-danger py-2 small mt-2 mb-0">{{ sourceError }}</div>
            </div>

            <FieldList v-model:selected="fields" :mapping="mapping" :timefield="timefield" />
          </div>
          <details class="help border-top">
            <summary class="px-3 py-2 small fw-semibold">Как это работает</summary>
            <ol class="small mb-0 px-3 pb-3 ps-4">
              <li>Выберите кластер и группу индексов.</li>
              <li>Задайте промежуток времени в UTC.</li>
              <li>При необходимости добавьте фильтры и строку поиска.</li>
              <li>Отметьте поля; без отметок выгружаются все.</li>
              <li>Нажмите «Найти»: записи подгружаются при прокрутке.</li>
              <li>Если результат подходит, выгрузите его в CSV или JSON.</li>
            </ol>
          </details>
        </div>
      </aside>

      <main class="col-12 col-md-8 col-xl-9 d-flex flex-column gap-3">
        <!-- Запрос -->
        <form class="card" @submit.prevent="run">
          <div class="card-body d-flex flex-column gap-2">
            <div class="d-flex flex-wrap gap-2">
              <div class="input-group flex-grow-1 query">
                <span class="input-group-text"><Icon name="search" /></span>
                <input
                  v-model="xql"
                  class="form-control font-monospace"
                  type="search"
                  placeholder='Строка поиска: error -debug "connection refused"'
                  aria-label="Строка поиска"
                />
              </div>
              <div class="d-flex flex-wrap align-items-center gap-2 range">
                <input v-model="from" class="form-control" type="datetime-local" step="1" aria-label="Начало, UTC" :disabled="!timefield" @input="preset = null" />
                <span class="text-body-secondary d-none d-sm-inline">–</span>
                <input v-model="to" class="form-control" type="datetime-local" step="1" aria-label="Конец, UTC" :disabled="!timefield" @input="preset = null" />
                <span class="badge text-bg-light border d-none d-sm-inline" title="Время задаётся и показывается в UTC">UTC</span>
              </div>
              <button class="btn btn-primary" type="submit" :disabled="!group || searching">
                <Icon :name="searching ? 'spin' : 'search'" /> Найти
              </button>
            </div>
            <div class="d-flex flex-wrap align-items-center gap-2">
              <div class="btn-group btn-group-sm" role="group" aria-label="Быстрый выбор времени">
                <button
                  v-for="p in PRESETS"
                  :key="p.id"
                  type="button"
                  class="btn btn-outline-secondary"
                  :class="{ active: preset === p.id }"
                  :aria-pressed="preset === p.id"
                  :disabled="!timefield"
                  @click="setPreset(p.id)"
                >
                  {{ p.title }}
                </button>
              </div>
              <span
                v-for="[id, f] in filterList"
                :key="id"
                class="badge rounded-pill bg-opacity-75 chip d-inline-flex align-items-center gap-2"
                :class="filterLabel(f).not ? 'text-bg-danger' : 'text-bg-primary'"
              >
                <button type="button" class="chip-body" title="Изменить" @click="editing = { id, filter: f }">
                  <template v-if="f.operation === 'is_not'">не </template><span class="font-monospace">{{ filterLabel(f).field }}</span>{{ filterLabel(f).rest }}
                </button>
                <button type="button" class="btn-close btn-close-white" aria-label="Убрать фильтр" @click="removeFilter(id)"></button>
              </span>
              <button type="button" class="btn btn-sm btn-link text-decoration-none" :disabled="!group" @click="editing = { id: null }">
                <Icon name="plus" /> Фильтр
              </button>
            </div>
            <div v-if="!timefield && group && Object.keys(mapping).length" class="small text-body-secondary">
              В индексах нет поля с датой: поиск идёт без ограничения по времени.
            </div>
          </div>
        </form>

        <div v-if="searchError" class="alert alert-danger d-flex gap-2 py-2 small mb-0"><Icon name="alert" /><span>{{ searchError }}</span></div>

        <template v-if="searched">
          <!-- Итог и выгрузка -->
          <div class="d-flex flex-wrap align-items-center gap-2">
            <strong class="tabular">
              <template v-if="total !== null">{{ num(total) }} записей</template>
              <template v-else>{{ num(hits.length) }}+ записей</template>
            </strong>
            <span class="text-body-secondary small tabular">
              {{ searched.index }}<template v-if="took !== undefined"> · {{ seconds(took) }}</template>
            </span>
            <span v-if="limits" class="ms-auto small text-body-secondary d-none d-lg-inline">До {{ num(limits.rows) }} строк или {{ bytes(limits.size) }}</span>
            <div class="btn-group btn-group-sm" :class="{ 'ms-auto': !limits }" role="group" aria-label="Выгрузка">
              <button type="button" class="btn btn-outline-success" :disabled="!hits.length || exporting?.phase === 'running'" @click="doExport('csv')"><Icon name="csv" /> CSV</button>
              <button type="button" class="btn btn-outline-success" :disabled="!hits.length || exporting?.phase === 'running'" @click="doExport('json')"><Icon name="json" /> JSON</button>
            </div>
          </div>

          <div v-if="exporting?.phase === 'running'" class="alert alert-secondary d-flex align-items-center gap-2 py-2 small mb-0">
            <Icon name="spin" />
            <span>
              Готовим <code>{{ fileName }}</code><template v-if="total !== null">: {{ num(total) }} записей</template>. Большие выгрузки
              занимают до нескольких минут, страницу можно не закрывать.
            </span>
          </div>
          <div v-else-if="exporting?.phase === 'done'" class="alert alert-success d-flex align-items-center gap-2 py-2 small mb-0">
            <Icon name="checkCircle" />
            <span>
              Файл готов: <a class="font-monospace" :href="exportURL(exporting.fname, exporting.format)" :download="fileName">{{ fileName }}</a>
              <template v-if="exporting.result"> · {{ num(exporting.result.rows) }} записей, {{ bytes(exporting.result.size) }}</template>.
              Он хранится на сервере 1 час.
            </span>
          </div>
          <div v-if="exporting?.phase === 'done' && exporting.result?.truncated" class="alert alert-warning d-flex align-items-center gap-2 py-2 small mb-0">
            <Icon name="alert" />
            <span>
              Выгрузка остановлена лимитом<template v-if="limits"> ({{ num(limits.rows) }} строк или {{ bytes(limits.size) }})</template>: в файле не
              всё найденное. Сузьте промежуток времени или добавьте фильтры.
            </span>
          </div>
          <div v-else-if="exporting?.phase === 'error'" class="alert alert-danger d-flex align-items-center gap-2 py-2 small mb-0">
            <Icon name="alert" />
            <span>Не удалось выгрузить <code>{{ fileName }}</code>: {{ exporting.error }}</span>
          </div>

          <ResultsTable
            v-if="hits.length"
            :hits="hits"
            :columns="columns"
            :timefield="timefield || undefined"
            :terms="terms"
            :total="total"
            :more="!!next"
            :loading="loadingMore"
            @more="loadMore"
          />
          <div v-else class="card empty">Ничего не нашлось. Расширьте промежуток времени или ослабьте фильтры.</div>
        </template>
        <div v-else-if="!searching && !searchError" class="card empty">
          Выберите индексы и нажмите «Найти». Записи подгружаются при прокрутке, всё найденное можно выгрузить в CSV или JSON.
        </div>
      </main>
    </div>
  </div>

  <FilterDialog
    v-if="editing"
    :fields="Object.keys(mapping).sort()"
    :initial="editing.filter"
    @save="saveFilter"
    @remove="editing.id && removeFilter(editing.id)"
    @close="editing = null"
  />
</template>

<style scoped>
.query {
  min-width: 260px;
}

.range input {
  width: 13.5rem;
}

@media (max-width: 575.98px) {
  .range,
  .range input {
    width: 100%;
  }
}

.chip {
  font-weight: 500;
  font-size: 0.8rem;
}

.chip-body {
  border: 0;
  background: none;
  color: inherit;
  padding: 0;
  font: inherit;
}

.chip .btn-close {
  width: 0.5em;
  height: 0.5em;
}

.help summary {
  cursor: pointer;
}
</style>
