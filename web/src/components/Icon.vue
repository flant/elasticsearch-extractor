<script setup lang="ts">
// Иконки Bootstrap Icons по коротким именам; "spin" - индикатор загрузки.
import { computed } from 'vue'

const props = defineProps<{ name: string }>()

const ICONS: Record<string, string> = {
  search: 'bi-search',
  filter: 'bi-funnel',
  refresh: 'bi-arrow-clockwise',
  auto: 'bi-circle-half',
  light: 'bi-sun',
  dark: 'bi-moon-stars',
  box: 'bi-box',
  boxes: 'bi-box-seam',
  cloud: 'bi-cloud',
  disk: 'bi-hdd',
  extract: 'bi-box-arrow-in-down',
  trash: 'bi-trash',
  open: 'bi-box-arrow-up-right',
  close: 'bi-x-lg',
  info: 'bi-info-circle',
  alert: 'bi-exclamation-triangle',
  clock: 'bi-clock-history',
  hourglass: 'bi-hourglass-split',
  check: 'bi-check-lg',
  checkCircle: 'bi-check-circle',
  plus: 'bi-plus-lg',
  chev: 'bi-chevron-right',
  up: 'bi-arrow-up',
  down: 'bi-arrow-down',
  csv: 'bi-filetype-csv',
  json: 'bi-filetype-json',
}

const cls = computed(() => ICONS[props.name] ?? 'bi-question-circle')
</script>

<template>
  <span v-if="name === 'spin'" class="spinner-border spinner-border-sm" aria-hidden="true"></span>
  <i v-else class="bi" :class="cls" aria-hidden="true"></i>
</template>
