<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Hit } from '../api'
import { display, docTime, fieldValue, flatten, highlight, num } from '../format'
import Icon from '../components/Icon.vue'

const props = defineProps<{
  hits: Hit[]
  columns: string[]
  timefield?: string
  terms: string[]
  total: number | null
  // У сервера есть следующая страница и она загружается
  more?: boolean
  loading?: boolean
}>()
const emit = defineEmits<{ more: [] }>()

// Записи дорисовываются порциями при прокрутке: сотни строк сразу заметно тормозят
// страницу. Когда загруженные кончаются, следующая страница запрашивается у сервера.
const STEP = 50
const limit = ref(STEP)
const open = ref(new Set<number>())
const sentinel = ref<HTMLElement>()
let observer: IntersectionObserver | undefined

const visible = computed(() => props.hits.slice(0, limit.value))
// Без выбранных полей вместо колонок показываем документ одной строкой.
const showDoc = computed(() => props.columns.length === 0)

// Новый поиск - сначала; дозагрузка той же выдачи сохраняет прокрутку и раскрытые строки
watch(
  () => props.hits,
  (now, before) => {
    if (before && now.length > before.length && now[0] === before[0]) return
    limit.value = STEP
    open.value = new Set()
  },
)

function toggle(i: number) {
  const s = new Set(open.value)
  if (s.has(i)) s.delete(i)
  else s.add(i)
  open.value = s
}

// Поля уровня (level, severity) показываются бейджем, если значение похоже на уровень
const LEVELS: Record<string, string> = {
  fatal: 'text-bg-danger', critical: 'text-bg-danger', error: 'text-bg-danger', err: 'text-bg-danger',
  warn: 'text-bg-warning', warning: 'text-bg-warning',
  info: 'text-bg-secondary', notice: 'text-bg-secondary', debug: 'text-bg-light border', trace: 'text-bg-light border',
}

function levelClass(field: string, value: string): string | undefined {
  return /level|severity/i.test(field) ? LEVELS[value.toLowerCase()] : undefined
}

function cell(hit: Hit, field: string): string {
  return display(fieldValue(hit, field))
}

function summary(hit: Hit): string {
  const pairs = flatten(hit._source ?? hit.fields ?? {}).filter(([k]) => k !== props.timefield)
  return pairs
    .slice(0, 12)
    .map(([k, v]) => `${k}: ${v}`)
    .join('  ·  ')
}

function details(hit: Hit): [string, string][] {
  const rows: [string, string][] = []
  if (hit._index) rows.push(['_index', hit._index])
  if (hit._source) rows.push(...flatten(hit._source))
  else if (hit.fields) rows.push(...Object.entries(hit.fields).map(([k, v]): [string, string] => [k, display(v)]))
  return rows
}

// Наблюдатель срабатывает только на изменение видимости: если после дозагрузки метка
// конца списка всё ещё на экране, переподписка проверит её снова.
watch(
  () => [limit.value, props.hits.length, props.loading],
  async () => {
    await nextTick()
    if (observer && sentinel.value) {
      observer.unobserve(sentinel.value)
      observer.observe(sentinel.value)
    }
  },
)

onMounted(() => {
  observer = new IntersectionObserver(
    (entries) => {
      if (!entries.some((e) => e.isIntersecting)) return
      if (limit.value < props.hits.length) limit.value += STEP
      else if (props.more && !props.loading) emit('more')
    },
    { rootMargin: '400px' },
  )
  if (sentinel.value) observer.observe(sentinel.value)
})
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <div class="card">
    <div class="table-responsive">
      <table class="table table-sm table-hover align-top mb-0">
        <thead class="table-light">
          <tr>
            <th class="expand"></th>
            <th v-if="timefield">{{ timefield }}</th>
            <th v-for="c in columns" :key="c">{{ c }}</th>
            <th v-if="showDoc">Документ</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="(h, i) in visible" :key="h._id ?? i">
            <tr>
              <td class="expand">
                <button
                  type="button"
                  class="btn btn-sm btn-link p-0 text-body-secondary toggle"
                  :aria-label="open.has(i) ? 'Свернуть' : 'Развернуть'"
                  :aria-expanded="open.has(i)"
                  @click="toggle(i)"
                >
                  <Icon name="chev" />
                </button>
              </td>
              <td v-if="timefield" class="time text-body-secondary">{{ docTime(fieldValue(h, timefield)) }}</td>
              <td v-for="c in columns" :key="c" class="msg">
                <span v-if="levelClass(c, cell(h, c))" class="badge" :class="levelClass(c, cell(h, c))">{{ cell(h, c) }}</span>
                <template v-else v-for="(s, j) in highlight(cell(h, c), terms)" :key="j"><mark v-if="s.hit">{{ s.text }}</mark><template v-else>{{ s.text }}</template></template>
              </td>
              <td v-if="showDoc" class="msg doc-line">
                <template v-for="(s, j) in highlight(summary(h), terms)" :key="j"><mark v-if="s.hit">{{ s.text }}</mark><template v-else>{{ s.text }}</template></template>
              </td>
            </tr>
            <tr v-if="open.has(i)">
              <td></td>
              <td :colspan="columns.length + (timefield ? 1 : 0) + (showDoc ? 1 : 0)" class="bg-body-tertiary">
                <dl class="row doc mb-0">
                  <template v-for="[k, v] in details(h)" :key="k">
                    <dt class="col-sm-4 col-lg-3 fw-normal text-body-secondary text-break">{{ k }}</dt>
                    <dd class="col-sm-8 col-lg-9 mb-1">{{ v }}</dd>
                  </template>
                </dl>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
    <div ref="sentinel" class="sentinel"></div>
  </div>
  <div class="small text-body-secondary d-flex align-items-center gap-2 tabular">
    <span v-if="loading" class="d-inline-flex align-items-center gap-2"><Icon name="spin" /> Загружаем следующие записи…</span>
    Показаны {{ num(visible.length) }} из {{ num(total ?? hits.length) }}
    <template v-if="visible.length < hits.length || more"> · прокрутка подгружает следующие</template>
  </div>
</template>

<style scoped>
.msg {
  font-family: var(--bs-font-monospace);
  font-size: 0.8rem;
  white-space: pre-wrap;
  word-break: break-word;
  min-width: 240px;
}

.doc-line {
  white-space: normal;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
  min-width: 320px;
}

td.time {
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}

.expand {
  width: 2rem;
}

.toggle :deep(i) {
  display: inline-block;
  transition: transform 0.12s;
}

.toggle[aria-expanded='true'] :deep(i) {
  transform: rotate(90deg);
}

.doc {
  font-family: var(--bs-font-monospace);
  font-size: 0.8rem;
}

.doc dd {
  word-break: break-all;
  white-space: pre-wrap;
}

.sentinel {
  height: 1px;
}
</style>
