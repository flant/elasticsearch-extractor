<script setup lang="ts">
import type { Node } from '../api'
import { bytes, percent } from '../format'

defineProps<{ nodes: Node[]; error?: string }>()

// После 75% диск заметно заполнен, после 90% восстановление может не поместиться.
function tone(p: number): string {
  return p >= 90 ? 'bg-danger' : p >= 75 ? 'bg-warning' : ''
}
</script>

<template>
  <div class="card">
    <div class="card-header section-title d-flex">Узлы <span class="badge text-bg-secondary ms-auto">{{ nodes.length }}</span></div>
    <div v-if="error" class="alert alert-danger m-2 py-2 small">{{ error }}</div>
    <ul class="list-group list-group-flush">
      <li v-for="n in nodes" :key="n.name" class="list-group-item">
        <div class="d-flex align-items-baseline gap-2">
          <strong>{{ n.name }}</strong>
          <span class="text-body-secondary font-monospace small">{{ n.ip }}</span>
          <span class="ms-auto small text-nowrap tabular" :class="{ 'text-danger': n.percent >= 90 }">{{ bytes(n.avail) }} своб.</span>
        </div>
        <div class="progress progress-thin my-1" role="progressbar" :aria-valuenow="n.percent" aria-valuemin="0" aria-valuemax="100" :aria-label="`Диск ${n.name}`">
          <div class="progress-bar" :class="tone(n.percent)" :style="{ width: `${Math.min(100, n.percent)}%` }"></div>
        </div>
        <div class="small-2 text-body-secondary tabular">{{ bytes(n.used) }} из {{ bytes(n.total) }} · {{ percent(n.percent) }}</div>
      </li>
    </ul>
  </div>
</template>
