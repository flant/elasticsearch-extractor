// Общие для страниц типы и помощники интерфейса.
import { ApiError, SessionError } from './api'

export interface ToastMessage {
  tone: 'ok' | 'warn' | 'bad'
  text: string
}

// Текст ошибки для человека. Потерянную сессию страница показывает отдельно.
export function errorMessage(e: unknown): string {
  if (e instanceof SessionError) return 'сессия входа истекла, обновите страницу'
  if (e instanceof ApiError) return e.message
  return e instanceof Error ? e.message : String(e)
}
