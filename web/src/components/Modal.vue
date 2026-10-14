<script setup lang="ts">
// Модальное окно Bootstrap, которым управляет Vue: закрывается по Esc и по клику мимо,
// фокус - на первом поле.
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

defineProps<{ title: string; role?: 'dialog' | 'alertdialog' }>()
const emit = defineEmits<{ close: [] }>()

const box = ref<HTMLElement>()
const titleId = `modal-${Math.random().toString(36).slice(2, 8)}`

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

onMounted(async () => {
  document.addEventListener('keydown', onKey)
  document.body.classList.add('modal-open')
  await nextTick()
  box.value?.querySelector<HTMLElement>('.modal-body input, .modal-body select, .modal-footer .btn-primary, .modal-footer .btn-danger')?.focus()
})
onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKey)
  document.body.classList.remove('modal-open')
})
</script>

<template>
  <div class="modal-backdrop fade show"></div>
  <div class="modal fade show" tabindex="-1" :role="role ?? 'dialog'" aria-modal="true" :aria-labelledby="titleId" @click.self="emit('close')">
    <div ref="box" class="modal-dialog modal-dialog-centered">
      <div class="modal-content">
        <div class="modal-header">
          <h2 :id="titleId" class="modal-title h5">{{ title }}</h2>
          <button type="button" class="btn-close" aria-label="Закрыть" @click="emit('close')"></button>
        </div>
        <div class="modal-body d-flex flex-column gap-3"><slot /></div>
        <div class="modal-footer"><slot name="footer" /></div>
      </div>
    </div>
  </div>
</template>
