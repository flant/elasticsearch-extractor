<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  SessionError,
  deleteIndex,
  getClusters,
  getNodes,
  getRepositories,
  getRestored,
  getSnapshots,
  kibanaURL,
  type Cluster,
  type Node,
  type Repository,
  type RestoreResult,
  type Restored,
  type Snapshot,
} from '../api'
import { errorMessage, type ToastMessage } from '../ui'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import Toast from '../components/Toast.vue'
import TopBar from '../components/TopBar.vue'
import NodeList from './NodeList.vue'
import RestoreDrawer from './RestoreDrawer.vue'
import RestoredList from './RestoredList.vue'
import SnapshotTable from './SnapshotTable.vue'

const kibana = kibanaURL()

const cluster = ref<Cluster>()
const repos = ref<Repository[]>([])
const reposError = ref('')
const repo = ref('')
const snapshots = ref<Snapshot[]>([])
const snapshotsError = ref('')
const snapshotsLoading = ref(false)
const nodes = ref<Node[]>([])
const nodesError = ref('')
const restored = ref<Restored[]>([])
const restoredError = ref('')
const now = ref(Date.now())

const filter = ref('')
const sortBy = ref<'date' | 'name'>('date')
const sortDesc = ref(true)

const extracting = ref<Snapshot | null>(null)
const deleting = ref<string | null>(null)
const deleteBusy = ref(false)
const toast = ref<ToastMessage | null>(null)
const sessionLost = ref(false)

const loading = computed(() => snapshotsLoading.value)
const free = computed(() => nodes.value.reduce((s, n) => s + n.avail, 0))

const shown = computed(() => {
  const f = filter.value.trim().toLowerCase()
  const list = f ? snapshots.value.filter((s) => s.name.toLowerCase().includes(f)) : [...snapshots.value]
  const dir = sortDesc.value ? -1 : 1
  if (sortBy.value === 'date') list.sort((a, b) => (a.date - b.date || a.name.localeCompare(b.name)) * dir)
  else list.sort((a, b) => a.name.localeCompare(b.name) * dir)
  return list
})

function setSort(by: 'date' | 'name') {
  if (sortBy.value === by) sortDesc.value = !sortDesc.value
  else {
    sortBy.value = by
    sortDesc.value = by === 'date'
  }
}

// Ошибку потерянной сессии показываем одной плашкой, остальные - у своего блока.
function fail(e: unknown, target: { value: string }) {
  if (e instanceof SessionError) sessionLost.value = true
  target.value = errorMessage(e)
}

/* ---------- Загрузка ---------- */

async function loadRepos() {
  reposError.value = ''
  try {
    repos.value = (await getRepositories()).sort((a, b) => a.id.localeCompare(b.id))
  } catch (e) {
    fail(e, reposError)
  }
}

async function loadSnapshots() {
  if (!repo.value) return
  const current = repo.value
  snapshotsLoading.value = true
  snapshotsError.value = ''
  try {
    const list = await getSnapshots(current)
    if (repo.value === current) snapshots.value = list
  } catch (e) {
    if (repo.value === current) {
      snapshots.value = []
      fail(e, snapshotsError)
    }
  } finally {
    if (repo.value === current) snapshotsLoading.value = false
  }
}

async function loadNodes() {
  try {
    nodes.value = await getNodes()
    nodesError.value = ''
  } catch (e) {
    fail(e, nodesError)
  }
}

async function loadRestored() {
  try {
    restored.value = await getRestored()
    restoredError.value = ''
  } catch (e) {
    fail(e, restoredError)
  }
  now.value = Date.now()
}

async function loadCluster() {
  try {
    cluster.value = (await getClusters()).find((c) => c.type === 'Snapshot')
  } catch {
    // имя кластера в шапке - не главное
  }
}

function selectRepo(id: string) {
  if (repo.value === id) return
  repo.value = id
  snapshots.value = []
  filter.value = ''
  const url = new URL(window.location.href)
  url.searchParams.set('repo', id)
  history.replaceState(null, '', url)
  loadSnapshots()
}

async function refreshAll() {
  await Promise.all([loadRepos(), loadNodes(), loadRestored(), loadSnapshots()])
}

/* ---------- Действия ---------- */

function onRestored(res: RestoreResult) {
  if (!res.skipped.length) {
    extracting.value = null
    toast.value = { tone: 'ok', text: `Запущено восстановление: ${res.restoring.length}. Прогресс - в списке справа.` }
  } else if (!res.restoring.length) {
    toast.value = { tone: 'bad', text: 'Ни один индекс не восстановлен: не хватило места на узлах.' }
  } else {
    toast.value = { tone: 'warn', text: `Восстанавливается ${res.restoring.length}, не хватило места для ${res.skipped.length}.` }
  }
  loadRestored()
  loadNodes()
}

async function confirmDelete() {
  if (!deleting.value) return
  const name = deleting.value
  deleteBusy.value = true
  try {
    await deleteIndex(name)
    toast.value = { tone: 'ok', text: `Индекс ${name} удалён` }
    deleting.value = null
    await Promise.all([loadRestored(), loadNodes()])
  } catch (e) {
    if (e instanceof SessionError) sessionLost.value = true
    toast.value = { tone: 'bad', text: `Не удалось удалить ${name}: ${errorMessage(e)}` }
    deleting.value = null
  } finally {
    deleteBusy.value = false
  }
}

/* ---------- Обновление ---------- */

function reload() {
  window.location.reload()
}

// Пока что-то восстанавливается, прогресс опрашиваем часто, иначе - раз в минуту.
let restoredTimer: number | undefined
let nodesTimer: number | undefined

function scheduleRestored() {
  const busy = restored.value.some((r) => !r.done)
  restoredTimer = window.setTimeout(async () => {
    if (!document.hidden) await loadRestored()
    scheduleRestored()
  }, busy ? 5_000 : 60_000)
}

onMounted(async () => {
  loadCluster()
  await loadRepos()
  const wanted = new URL(window.location.href).searchParams.get('repo')
  const first = repos.value.find((r) => r.id === wanted) ?? repos.value[0]
  if (first) {
    repo.value = first.id
    loadSnapshots()
  }
  await Promise.all([loadNodes(), loadRestored()])
  scheduleRestored()
  nodesTimer = window.setInterval(() => {
    if (!document.hidden) loadNodes()
  }, 60_000)
})

onBeforeUnmount(() => {
  clearTimeout(restoredTimer)
  clearInterval(nodesTimer)
})
</script>

<template>
  <TopBar page="snapshots" :cluster="cluster?.name" :host="cluster?.host" :loading="loading" @refresh="refreshAll" />
  <div v-if="sessionLost" class="alert alert-danger d-flex align-items-center gap-2 m-3 mb-0">
    <Icon name="alert" /> Сессия входа истекла или сервер недоступен.
    <button class="btn btn-sm btn-outline-danger ms-auto" @click="reload">Обновить страницу</button>
  </div>

  <div class="container-fluid py-3">
    <div class="row g-3">
      <!-- Слева: репозитории и узлы -->
      <aside class="col-12 col-md-4 col-xl-3">
        <div class="card mb-3">
          <div class="card-header section-title">Репозитории</div>
          <div v-if="reposError" class="alert alert-danger m-2 py-2 small">{{ reposError }}</div>
          <div class="list-group list-group-flush">
            <button
              v-for="r in repos"
              :key="r.id"
              type="button"
              class="list-group-item list-group-item-action d-flex align-items-center gap-2"
              :class="{ active: r.id === repo }"
              :aria-current="r.id === repo ? 'true' : undefined"
              :title="r.type"
              @click="selectRepo(r.id)"
            >
              <Icon :name="r.type === 's3' ? 'cloud' : 'disk'" />
              {{ r.id }}
              <span v-if="r.id === repo && snapshots.length" class="badge text-bg-light ms-auto">
                {{ snapshots.length }}
              </span>
            </button>
          </div>
          <div v-if="!repos.length && !reposError" class="empty">Репозиториев нет</div>
        </div>
        <NodeList :nodes="nodes" :error="nodesError" />
      </aside>

      <!-- Центр: снапшоты -->
      <main class="col-12 col-md-8 col-xl-6">
        <div class="d-flex flex-wrap align-items-center gap-2 mb-3">
          <h1 class="h4 mb-0">
            Снапшоты <span v-if="repo" class="fw-normal text-body-secondary">· {{ repo }}</span>
          </h1>
          <div class="ms-auto d-flex flex-wrap gap-2">
            <div class="input-group input-group-sm filter">
              <span class="input-group-text"><Icon name="search" /></span>
              <input v-model="filter" class="form-control" type="search" placeholder="Фильтр по имени" aria-label="Фильтр по имени" />
            </div>
            <div class="btn-group btn-group-sm" role="group" aria-label="Сортировка">
              <button type="button" class="btn btn-outline-secondary" :class="{ active: sortBy === 'date' }" :aria-pressed="sortBy === 'date'" @click="setSort('date')">
                По дате <Icon v-if="sortBy === 'date'" :name="sortDesc ? 'down' : 'up'" />
              </button>
              <button type="button" class="btn btn-outline-secondary" :class="{ active: sortBy === 'name' }" :aria-pressed="sortBy === 'name'" @click="setSort('name')">
                По имени <Icon v-if="sortBy === 'name'" :name="sortDesc ? 'down' : 'up'" />
              </button>
            </div>
          </div>
        </div>

        <div class="alert alert-info d-flex gap-2 py-2 small" role="note">
          <Icon name="info" />
          <span>Снапшот за дату содержит индексы <strong>предыдущего</strong> дня: в снапшоте за 13 октября лежат логи за 12 октября.</span>
        </div>

        <div v-if="snapshotsError" class="alert alert-danger d-flex gap-2 py-2 small"><Icon name="alert" /><span>{{ snapshotsError }}</span></div>
        <div v-if="snapshotsLoading && !snapshots.length" class="card empty"><span><Icon name="spin" /> Загружаем снапшоты…</span></div>
        <template v-else-if="shown.length">
          <SnapshotTable :snapshots="shown" :selected="extracting?.name" @extract="(s) => (extracting = s)" />
          <div class="small text-body-secondary mt-2 tabular">
            <template v-if="filter">Показано {{ shown.length }} из {{ snapshots.length }}</template>
            <template v-else>Всего {{ snapshots.length }}</template>
          </div>
        </template>
        <div v-else-if="!snapshotsError && repo" class="card empty">
          {{ filter ? 'Нет снапшотов по фильтру' : 'В репозитории нет снапшотов с датой в имени' }}
        </div>
      </main>

      <!-- Справа: восстановленные индексы -->
      <aside class="col-12 col-xl-3">
        <RestoredList :items="restored" :kibana="kibana" :now="now" :error="restoredError" @delete="(n) => (deleting = n)" />
      </aside>
    </div>
  </div>

  <RestoreDrawer
    v-if="extracting"
    :key="extracting.name"
    :repo="repo"
    :snapshot="extracting"
    :free="free"
    :nodes="nodes.length"
    @close="extracting = null"
    @restored="onRestored"
  />

  <Modal v-if="deleting" title="Удалить индекс?" role="alertdialog" @close="deleting = null">
    <p class="font-monospace small mb-0">{{ deleting }}</p>
    <p class="text-body-secondary mb-0">Индекс удалится из кластера сразу. Данные останутся в снапшоте, их можно будет извлечь снова.</p>
    <template #footer>
      <button type="button" class="btn btn-outline-secondary" @click="deleting = null">Отмена</button>
      <button type="button" class="btn btn-danger" :disabled="deleteBusy" @click="confirmDelete">
        <Icon :name="deleteBusy ? 'spin' : 'trash'" /> Удалить
      </button>
    </template>
  </Modal>

  <Toast :message="toast" @close="toast = null" />
</template>

<style scoped>
.filter {
  width: 220px;
}
</style>
