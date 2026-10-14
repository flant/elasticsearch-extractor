<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ApiError, getSnapshotIndices, restore, type RestoreResult, type Snapshot, type SnapshotIndex } from '../api'
import { bytes, day, num } from '../format'
import { errorMessage } from '../ui'
import Icon from '../components/Icon.vue'
import { badgeClass, logsDay, stateView } from './status'

const props = defineProps<{ repo: string; snapshot: Snapshot; free: number; nodes: number }>()
const emit = defineEmits<{ close: []; restored: [res: RestoreResult] }>()

const indices = ref<SnapshotIndex[]>([])
const loading = ref(true)
const error = ref('')
const filter = ref('')
const selected = ref(new Set<string>())
const busy = ref(false)
const result = ref<RestoreResult | null>(null)

const view = computed(() => stateView(props.snapshot.state))
const shown = computed(() => {
  const f = filter.value.trim().toLowerCase()
  return f ? indices.value.filter((i) => i.name.toLowerCase().includes(f)) : indices.value
})
const total = computed(() => indices.value.reduce((s, i) => s + i.size, 0))
const chosen = computed(() => indices.value.filter((i) => selected.value.has(i.name)))
const chosenSize = computed(() => chosen.value.reduce((s, i) => s + i.size, 0))
const allShown = computed(() => shown.value.length > 0 && shown.value.every((i) => selected.value.has(i.name)))

// Имя, под которым backend восстановит индекс: extracted_<имя>-DD-MM-YYYY по сегодняшней дате.
const suffix = (() => {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getDate())}-${p(d.getMonth() + 1)}-${d.getFullYear()}`
})()

function toggle(name: string) {
  const s = new Set(selected.value)
  if (s.has(name)) s.delete(name)
  else s.add(name)
  selected.value = s
}

function toggleAll() {
  const s = new Set(selected.value)
  const on = !allShown.value
  for (const i of shown.value) {
    if (on) s.add(i.name)
    else s.delete(i.name)
  }
  selected.value = s
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    indices.value = await getSnapshotIndices(props.repo, props.snapshot.name)
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

async function run() {
  if (!chosen.value.length || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const res = await restore(
      props.repo,
      props.snapshot.name,
      chosen.value.map((i) => i.name),
    )
    // Если места не хватило части индексов, панель остаётся открытой и показывает их отдельно.
    if (res.skipped.length) {
      result.value = res
      selected.value = new Set()
    }
    emit('restored', res)
  } catch (e) {
    error.value =
      e instanceof ApiError && e.status === 429
        ? 'В кластере ещё восстанавливаются другие индексы. Повторите, когда они закончат.'
        : errorMessage(e)
  } finally {
    busy.value = false
  }
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

onMounted(() => {
  document.addEventListener('keydown', onKey)
  load()
})
onBeforeUnmount(() => document.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="offcanvas-backdrop fade show" @click="emit('close')"></div>
  <aside class="offcanvas offcanvas-end show restore" tabindex="-1" role="dialog" aria-modal="true" aria-labelledby="drawer-title">
    <div class="offcanvas-header border-bottom align-items-start">
      <div>
        <h2 id="drawer-title" class="offcanvas-title h5 font-monospace">{{ snapshot.name }}</h2>
        <div class="small text-body-secondary">
          {{ repo }} · логи за {{ day(logsDay(snapshot.date)) }}
          <span v-if="view" class="badge" :class="badgeClass(view)">{{ view.title }}</span>
        </div>
      </div>
      <button type="button" class="btn-close" aria-label="Закрыть" @click="emit('close')"></button>
    </div>

    <div class="offcanvas-body d-flex flex-column gap-3">
      <div v-if="result" class="alert alert-warning d-flex gap-2 py-2 small mb-0">
        <Icon name="alert" />
        <span>
          Не хватило места на узлах, эти индексы не восстановлены:
          <code>{{ result.skipped.join(', ') }}</code>.
          <template v-if="result.restoring.length"> Остальные ({{ result.restoring.length }}) восстанавливаются.</template>
        </span>
      </div>
      <div v-if="error" class="alert alert-danger d-flex gap-2 py-2 small mb-0"><Icon name="alert" /><span>{{ error }}</span></div>

      <div class="d-flex align-items-center gap-2">
        <div class="input-group input-group-sm">
          <span class="input-group-text"><Icon name="search" /></span>
          <input v-model="filter" class="form-control" type="search" placeholder="Фильтр индексов" aria-label="Фильтр индексов" />
        </div>
        <span class="small text-body-secondary text-nowrap tabular">{{ num(indices.length) }} индексов · {{ bytes(total) }}</span>
      </div>

      <div v-if="loading" class="empty"><Icon name="spin" /> Читаем состав снапшота…</div>
      <div v-else-if="indices.length" class="border rounded idx-table">
        <table class="table table-sm table-hover align-middle mb-0">
          <thead class="table-light">
            <tr>
              <th class="check"><input class="form-check-input" type="checkbox" :checked="allShown" aria-label="Выбрать все показанные" @change="toggleAll" /></th>
              <th>Индекс</th>
              <th class="text-end">Шарды</th>
              <th class="text-end">Размер</th>
            </tr>
          </thead>
          <tbody class="font-monospace small">
            <tr v-for="i in shown" :key="i.name" :class="{ 'table-primary': selected.has(i.name) }" @click="toggle(i.name)">
              <td class="check"><input class="form-check-input" type="checkbox" :checked="selected.has(i.name)" :aria-label="i.name" @click.stop @change="toggle(i.name)" /></td>
              <td class="text-break">{{ i.name }}</td>
              <td class="text-end tabular">{{ i.shards }}</td>
              <td class="text-end tabular text-nowrap">{{ bytes(i.size) }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="!shown.length" class="empty">Нет индексов по фильтру</div>
      </div>
      <div v-else-if="!error" class="empty">В снапшоте нет индексов</div>

      <dl class="row small mb-0">
        <dt class="col-4 fw-normal text-body-secondary">Новое имя</dt>
        <dd class="col-8 font-monospace">extracted_&lt;индекс&gt;-{{ suffix }}</dd>
        <dt class="col-4 fw-normal text-body-secondary">Реплики</dt>
        <dd class="col-8">0, алиасы и глобальное состояние не восстанавливаются</dd>
        <dt class="col-4 fw-normal text-body-secondary">Свободно</dt>
        <dd class="col-8 mb-0 tabular">{{ bytes(free) }} на {{ nodes }} {{ nodes === 1 ? 'узле' : 'узлах' }}</dd>
      </dl>
      <div class="alert alert-secondary d-flex gap-2 py-2 small mb-0">
        <Icon name="info" />
        <span>Индексы, для которых не хватит места на узлах, восстановлены не будут, сервер сообщит о них отдельно.</span>
      </div>
    </div>

    <div class="border-top p-3 d-flex align-items-center gap-2">
      <span class="me-auto small tabular">
        <strong>Выбрано {{ chosen.length }}</strong> <span class="text-body-secondary">· {{ bytes(chosenSize) }}</span>
      </span>
      <button type="button" class="btn btn-sm btn-outline-secondary" @click="emit('close')">{{ result ? 'Закрыть' : 'Отмена' }}</button>
      <button type="button" class="btn btn-sm btn-primary" :disabled="!chosen.length || busy" @click="run">
        <Icon :name="busy ? 'spin' : 'extract'" /> Восстановить
      </button>
    </div>
  </aside>
</template>

<style scoped>
.restore {
  --bs-offcanvas-width: min(560px, 100vw);
}

.idx-table {
  max-height: 46vh;
  overflow: auto;
}

.idx-table thead th {
  position: sticky;
  top: 0;
  z-index: 1;
}

.idx-table tbody tr {
  cursor: pointer;
}

.check {
  width: 2rem;
}
</style>
