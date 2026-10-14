// Состояния снапшотов Elasticsearch и их подписи.

export interface StateView {
  title: string
  tone: 'ok' | 'warn' | 'bad' | 'info'
  icon?: string
  hint?: string
  // Можно ли извлекать индексы из такого снапшота
  usable: boolean
}

const STATES: Record<string, StateView> = {
  SUCCESS: { title: 'Готов', tone: 'ok', usable: true },
  PARTIAL: { title: 'Частично', tone: 'warn', hint: 'Часть шардов не сохранилась', usable: true },
  IN_PROGRESS: { title: 'Создаётся', tone: 'info', icon: 'hourglass', usable: false },
  FAILED: { title: 'Ошибка', tone: 'bad', usable: false },
  INCOMPATIBLE: { title: 'Несовместим', tone: 'bad', hint: 'Снапшот сделан несовместимой версией', usable: false },
}

export function stateView(state?: string): StateView | null {
  if (!state) return null
  return STATES[state] ?? { title: state, tone: 'warn', usable: true }
}

// Класс бейджа Bootstrap для состояния.
export function badgeClass(v: StateView): string {
  return { ok: 'text-bg-success', warn: 'text-bg-warning', bad: 'text-bg-danger', info: 'text-bg-info' }[v.tone]
}

export const DAY = 24 * 60 * 60 * 1000

// Снапшот за дату содержит индексы предыдущего дня.
export function logsDay(snapshotDate: number): number {
  return snapshotDate - DAY
}
