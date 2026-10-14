<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from '../components/Icon.vue'

const props = defineProps<{ mapping: Record<string, string>; selected: string[]; timefield?: string }>()
const emit = defineEmits<{ 'update:selected': [fields: string[]] }>()

const filter = ref('')

// Поле времени - первым, остальные по алфавиту.
const fields = computed(() => {
  const f = filter.value.trim().toLowerCase()
  return Object.keys(props.mapping)
    .filter((n) => !f || n.toLowerCase().includes(f))
    .sort((a, b) => (a === props.timefield ? -1 : b === props.timefield ? 1 : a.localeCompare(b)))
})

const count = computed(() => props.selected.length)

function toggle(name: string) {
  const s = props.selected.includes(name) ? props.selected.filter((x) => x !== name) : [...props.selected, name]
  emit('update:selected', s)
}
</script>

<template>
  <div>
    <div class="d-flex align-items-baseline mb-2">
      <span class="section-title">Поля</span>
      <span v-if="count" class="ms-auto small-2 text-body-secondary">
        выбрано {{ count }} · <a href="#" @click.prevent="emit('update:selected', [])">сбросить</a>
      </span>
      <span v-else class="ms-auto small-2 text-body-secondary">все</span>
    </div>
    <div class="input-group input-group-sm mb-2">
      <span class="input-group-text"><Icon name="filter" /></span>
      <input v-model="filter" class="form-control" type="search" placeholder="Фильтр полей" aria-label="Фильтр полей" />
    </div>
    <div class="fields">
      <div v-for="(f, i) in fields" :key="f" class="form-check d-flex align-items-center gap-2" :title="f">
        <input
          :id="`field-${i}`"
          class="form-check-input"
          type="checkbox"
          :checked="f === timefield || selected.includes(f)"
          :disabled="f === timefield"
          @change="toggle(f)"
        />
        <label class="form-check-label text-truncate" :for="`field-${i}`">{{ f }}</label>
        <span v-if="f === timefield" class="badge text-bg-primary ms-auto">время</span>
        <span v-else class="badge border text-body-secondary ms-auto">{{ mapping[f] }}</span>
      </div>
    </div>
    <div v-if="!fields.length" class="empty">{{ filter ? 'Нет полей по фильтру' : 'Нет полей' }}</div>
  </div>
</template>

<style scoped>
.fields {
  max-height: 46vh;
  overflow: auto;
}

.form-check-label {
  font-family: var(--bs-font-monospace);
  font-size: 0.8rem;
  min-width: 0;
}
</style>
