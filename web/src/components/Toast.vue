<script setup lang="ts">
// Короткое сообщение внизу экрана (toast Bootstrap). Ошибки и предупреждения не исчезают сами.
import { computed, onBeforeUnmount, watch } from 'vue'
import type { ToastMessage } from '../ui'
import Icon from './Icon.vue'

const props = defineProps<{ message: ToastMessage | null }>()
const emit = defineEmits<{ close: [] }>()

const bg = computed(() => ({ ok: 'text-bg-success', warn: 'text-bg-warning', bad: 'text-bg-danger' })[props.message?.tone ?? 'ok'])

let timer: number | undefined
watch(
  () => props.message,
  (m) => {
    clearTimeout(timer)
    if (m?.tone === 'ok') timer = window.setTimeout(() => emit('close'), 4000)
  },
)
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div class="toast-container position-fixed bottom-0 start-50 translate-middle-x p-3">
    <div v-if="message" class="toast show align-items-center border-0" :class="bg" role="status" aria-live="polite" aria-atomic="true">
      <div class="d-flex">
        <div class="toast-body d-flex gap-2">
          <Icon :name="message.tone === 'ok' ? 'check' : 'alert'" />
          <span>{{ message.text }}</span>
        </div>
        <button type="button" class="btn-close me-2 m-auto" :class="{ 'btn-close-white': message.tone !== 'warn' }" aria-label="Закрыть" @click="emit('close')"></button>
      </div>
    </div>
  </div>
</template>
