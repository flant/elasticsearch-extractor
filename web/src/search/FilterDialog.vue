<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Filter, Operation } from '../api'
import Modal from '../components/Modal.vue'

const props = defineProps<{ fields: string[]; initial?: Filter }>()
const emit = defineEmits<{ save: [f: Filter]; remove: []; close: [] }>()

const OPS: { id: Operation; title: string }[] = [
  { id: 'is', title: 'равно' },
  { id: 'is_not', title: 'не равно' },
  { id: 'exists', title: 'существует' },
  { id: 'does_not_exists', title: 'не существует' },
]

const field = ref(props.initial?.field ?? '')
const operation = ref<Operation>(props.initial?.operation ?? 'is')
const value = ref(props.initial?.value ?? '')

const needsValue = computed(() => operation.value === 'is' || operation.value === 'is_not')
const valid = computed(() => field.value.trim() !== '' && (!needsValue.value || value.value.trim() !== ''))

function save() {
  if (!valid.value) return
  emit('save', { field: field.value.trim(), operation: operation.value, value: needsValue.value ? value.value : '' })
}
</script>

<template>
  <Modal :title="initial ? 'Изменить фильтр' : 'Фильтр'" @close="emit('close')">
    <form id="filter-form" class="d-flex flex-column gap-3" @submit.prevent="save">
      <div>
        <label class="section-title form-label" for="filter-field">Поле</label>
        <input id="filter-field" v-model="field" class="form-control font-monospace" list="filter-fields" autocomplete="off" required />
        <datalist id="filter-fields">
          <option v-for="f in fields" :key="f" :value="f"></option>
        </datalist>
      </div>
      <div>
        <div class="section-title form-label">Условие</div>
        <div class="btn-group btn-group-sm flex-wrap" role="radiogroup" aria-label="Условие">
          <button
            v-for="o in OPS"
            :key="o.id"
            type="button"
            role="radio"
            class="btn btn-outline-primary"
            :class="{ active: operation === o.id }"
            :aria-checked="operation === o.id"
            @click="operation = o.id"
          >
            {{ o.title }}
          </button>
        </div>
      </div>
      <div v-if="needsValue">
        <label class="section-title form-label" for="filter-value">Значение</label>
        <input id="filter-value" v-model="value" class="form-control" required />
        <div class="form-text">«Равно» ищет фразу целиком (match_phrase).</div>
      </div>
    </form>
    <template #footer>
      <button v-if="initial" class="btn btn-outline-danger me-auto" type="button" @click="emit('remove')">Удалить</button>
      <button class="btn btn-outline-secondary" type="button" @click="emit('close')">Отмена</button>
      <button class="btn btn-primary" type="submit" form="filter-form" :disabled="!valid">{{ initial ? 'Сохранить' : 'Добавить' }}</button>
    </template>
  </Modal>
</template>
