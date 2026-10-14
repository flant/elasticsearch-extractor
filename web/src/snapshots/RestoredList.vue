<script setup lang="ts">
import { computed } from 'vue'
import type { Restored } from '../api'
import { bytes, dateTime, shortDay } from '../format'
import Icon from '../components/Icon.vue'
import { DAY } from './status'

const props = defineProps<{ items: Restored[]; kibana?: string; now: number; error?: string }>()
const emit = defineEmits<{ delete: [name: string] }>()

// Имя восстановленного индекса: extracted_<исходный>-DD-MM-YYYY. Curator удаляет такие индексы,
// когда дате в имени больше двух суток.
function expires(name: string): number | null {
  const m = /-(\d{2})-(\d{2})-(\d{4})$/.exec(name)
  return m ? Date.UTC(+m[3], +m[2] - 1, +m[1]) + 2 * DAY : null
}

function source(r: Restored): string {
  if (r.source) return r.source
  return r.name.replace(/^extracted_/, '').replace(/-\d{2}-\d{2}-\d{4}$/, '')
}

function left(name: string): string {
  const e = expires(name)
  if (e === null) return ''
  const h = Math.floor((e - props.now) / 3600_000)
  if (h <= 0) return 'скоро удалится'
  return h < 48 ? `ещё ${h} ч` : `до ${shortDay(e)}`
}

const discover = computed(() => (props.kibana ? `${props.kibana}/app/discover#` : undefined))
</script>

<template>
  <div class="card">
    <div class="card-header section-title d-flex">
      Восстановленные индексы <span class="badge text-bg-secondary ms-auto">{{ items.length }}</span>
    </div>
    <div class="card-body pb-0">
      <div class="alert alert-warning d-flex gap-2 py-2 small mb-0">
        <Icon name="clock" />
        <span>Индексы <code>extracted_*</code> удаляются через 48 часов после восстановления.</span>
      </div>
      <div v-if="error" class="alert alert-danger py-2 small mt-2 mb-0">{{ error }}</div>
    </div>
    <div v-if="!error && !items.length" class="empty">Пока ничего не восстановлено</div>
    <ul class="list-group list-group-flush mt-2">
      <li v-for="r in items" :key="r.name" class="list-group-item">
        <div class="font-monospace small fw-semibold text-break" :title="r.name">{{ source(r) }}</div>
        <div v-if="r.snapshot || r.started" class="small-2 text-body-secondary">
          <template v-if="r.snapshot">из {{ r.snapshot }}</template><template v-if="r.snapshot && r.started"> · </template><template v-if="r.started">{{ dateTime(r.started) }}</template>
        </div>
        <div class="progress progress-thin my-2" role="progressbar" :aria-valuenow="r.percent" aria-valuemin="0" aria-valuemax="100" aria-label="Восстановление">
          <div class="progress-bar" :class="r.done ? 'bg-success' : 'progress-bar-striped progress-bar-animated'" :style="{ width: `${r.percent}%` }"></div>
        </div>
        <div v-if="r.done" class="d-flex align-items-center gap-2 small">
          <span class="badge text-bg-success"><Icon name="check" /> Готов</span>
          <span class="text-body-secondary text-nowrap tabular" :title="r.name">{{ bytes(r.total) }}<template v-if="left(r.name)"> · {{ left(r.name) }}</template></span>
          <span class="ms-auto btn-group btn-group-sm">
            <a v-if="discover" class="btn btn-outline-secondary" :href="discover" target="_blank" rel="noopener" title="Открыть в Kibana" aria-label="Открыть в Kibana">
              <Icon name="open" />
            </a>
            <button type="button" class="btn btn-outline-danger" title="Удалить" aria-label="Удалить" @click="emit('delete', r.name)">
              <Icon name="trash" />
            </button>
          </span>
        </div>
        <div v-else class="small text-primary tabular">
          Восстанавливается · {{ r.percent }}% <span class="text-body-secondary">· {{ bytes(r.recovered) }} из {{ bytes(r.total) }}</span>
        </div>
      </li>
    </ul>
  </div>
</template>
