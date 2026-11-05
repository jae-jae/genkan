import { computed, signal } from '@preact/signals'
import { get } from './api'
import { t } from './i18n'
import { isAdmin, toast } from './store'
import type { UpdateStatus } from './types'

/** Last `GET /api/update` answer (admin only; null until the first one arrives, or when the server has no such endpoint). */
export const upd = signal<UpdateStatus | null>(null)

/** The topbar button: a newer release exists and the admin did not skip it (docs/UPDATES.md §7). */
export const updateBadge = computed(() => {
  const s = upd.value
  return isAdmin.value && !!s && s.available && !!s.latest && s.latest !== s.dismissed
})

let told = false
/** Stores a status (also the answer of POST /api/update/*). */
export function setUpd(s: UpdateStatus | null | undefined) {
  if (!s || typeof s !== 'object') return
  upd.value = { ...s, releases: s.releases || [] }
  if (s.rolled_back && !told) {
    told = true
    toast(t('upd.rolledBackToast', { v: s.rolled_back }), 'error')
  }
}

/** Fetches the status; errors are ignored (older server, offline, restarting). */
export async function refreshUpdate(): Promise<UpdateStatus | null> {
  try {
    setUpd(await get<UpdateStatus>('/api/update'))
    return upd.value
  } catch {
    return null
  }
}

/** Loads the status once and then every hour while `on`. Returns the cleanup. */
export function watchUpdate(on: boolean): () => void {
  if (!on) {
    upd.value = null
    return () => {}
  }
  void refreshUpdate()
  const id = setInterval(refreshUpdate, 3600_000)
  return () => clearInterval(id)
}
