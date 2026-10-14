<script setup lang="ts">
import type { Snapshot } from '../api'
import { day } from '../format'
import Icon from '../components/Icon.vue'
import { badgeClass, logsDay, stateView } from './status'

defineProps<{ snapshots: Snapshot[]; selected?: string }>()
const emit = defineEmits<{ extract: [s: Snapshot] }>()
</script>

<template>
  <div class="card">
    <div class="table-responsive">
      <table class="table table-hover align-middle mb-0 text-nowrap">
        <thead class="table-light">
          <tr>
            <th>Снапшот</th>
            <th class="d-none d-sm-table-cell">Логи за</th>
            <th>Статус</th>
            <th class="text-end"><span class="visually-hidden">Действия</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in snapshots" :key="s.uuid || s.name" :class="{ 'table-active': s.name === selected }">
            <td class="font-monospace small"><span class="text-body-secondary me-1"><Icon name="box" /></span>{{ s.name }}</td>
            <td class="d-none d-sm-table-cell tabular">{{ day(logsDay(s.date)) }}</td>
            <td>
              <span v-if="stateView(s.state)" class="badge" :class="badgeClass(stateView(s.state)!)" :title="stateView(s.state)!.hint">
                <template v-if="stateView(s.state)!.icon"><Icon :name="stateView(s.state)!.icon!" /> </template>{{ stateView(s.state)!.title }}
              </span>
            </td>
            <td class="text-end">
              <button type="button" class="btn btn-sm btn-outline-primary" :disabled="stateView(s.state)?.usable === false" @click="emit('extract', s)">
                <Icon name="extract" /><span class="d-none d-sm-inline"> Извлечь</span>
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
