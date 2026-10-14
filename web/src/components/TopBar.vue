<script setup lang="ts">
import { kibanaURL } from '../api'
import { cycleTheme, theme, themeTitle } from '../theme'
import Icon from './Icon.vue'

defineProps<{ page: 'snapshots' | 'search'; cluster?: string; host?: string; loading?: boolean }>()
const emit = defineEmits<{ refresh: [] }>()

const kibana = kibanaURL()
</script>

<template>
  <nav class="navbar navbar-expand bg-body border-bottom sticky-top">
    <div class="container-fluid gap-3">
      <a class="navbar-brand d-flex align-items-center gap-2 me-0" href="/">
        <svg class="brand-logo" viewBox="0 0 16 16" aria-hidden="true">
          <path class="x-blue" d="M3.5 3.5 12.5 12.5" stroke-width="3" stroke-linecap="round" />
          <path class="x-green" d="M12.5 3.5 3.5 12.5" stroke-width="3" stroke-linecap="round" />
        </svg>
        <span class="fw-semibold d-none d-sm-inline">X-tractor</span>
      </a>
      <span v-if="cluster" class="navbar-text small-2 d-none d-lg-inline" :title="host">
        {{ cluster }}<template v-if="host"> · {{ host }}</template>
      </span>

      <ul class="nav nav-pills">
        <li class="nav-item">
          <a class="nav-link py-1" :class="{ active: page === 'snapshots' }" href="/" :aria-current="page === 'snapshots' ? 'page' : undefined">
            <Icon name="boxes" /> Снапшоты
          </a>
        </li>
        <li class="nav-item">
          <a class="nav-link py-1" :class="{ active: page === 'search' }" href="/search/" :aria-current="page === 'search' ? 'page' : undefined">
            <Icon name="search" /> Поиск
          </a>
        </li>
      </ul>

      <div class="ms-auto d-flex align-items-center gap-1">
        <a v-if="kibana" class="btn btn-sm btn-outline-secondary d-none d-md-inline-block" :href="kibana" target="_blank" rel="noopener">
          <Icon name="open" /> Kibana
        </a>
        <button class="btn btn-sm btn-link text-body" :title="themeTitle[theme]" :aria-label="themeTitle[theme]" @click="cycleTheme()">
          <Icon :name="theme" />
        </button>
        <button class="btn btn-sm btn-link text-body" title="Обновить" aria-label="Обновить" @click="emit('refresh')">
          <Icon :name="loading ? 'spin' : 'refresh'" />
        </button>
      </div>
    </div>
  </nav>
</template>
