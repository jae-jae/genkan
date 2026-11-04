import { loadWidgets, useChunk } from './lazy'
import { curWs, editMode, isAdmin, widgets } from './store'

/** Between Hero and Board. The widget code is a separate chunk, fetched only when this workspace has widgets (or the admin is editing). */
export function WidgetSlot() {
  const ws = curWs.value
  const any = !!ws && widgets.value.some((w) => !w.workspace_id || w.workspace_id === ws.id)
  const on = any || (editMode.value && isAdmin.value)
  const m = useChunk(loadWidgets, on)
  if (m && on) return <m.WidgetArea />
  // Edit mode with no widgets yet: reserve the room of the (lazy) area's "add widget" card so the board does not jump down
  // when the chunk arrives (it would break drags that started right after switching to edit mode).
  return on && !any ? <div class="widgets-ph" aria-hidden="true" /> : null
}
