// Тема интерфейса: как в системе, светлая или тёмная. Выбор помнит браузер пользователя,
// тема применяется атрибутом data-bs-theme Bootstrap.
import { ref } from 'vue'

export type Theme = 'auto' | 'light' | 'dark'

const KEY = 'xtractor.theme'
const system = window.matchMedia('(prefers-color-scheme: dark)')

function load(): Theme {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'light' || v === 'dark') return v
  } catch {
    // хранилище недоступно (приватный режим) - тема по системе
  }
  return 'auto'
}

export const theme = ref<Theme>(load())

export function applyTheme() {
  const t = theme.value === 'auto' ? (system.matches ? 'dark' : 'light') : theme.value
  document.documentElement.dataset.bsTheme = t
}

// Тема "как в системе" следует за сменой темы системы на лету
system.addEventListener('change', () => {
  if (theme.value === 'auto') applyTheme()
})

export function cycleTheme() {
  theme.value = ({ auto: 'light', light: 'dark', dark: 'auto' } as const)[theme.value]
  try {
    localStorage.setItem(KEY, theme.value)
  } catch {
    // не запомнили - не страшно
  }
  applyTheme()
}

export const themeTitle: Record<Theme, string> = {
  auto: 'Тема: как в системе',
  light: 'Тема: светлая',
  dark: 'Тема: тёмная',
}
