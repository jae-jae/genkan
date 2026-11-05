// Interactive widget blocks (docs/PLUGINS.md 15): list rows (check / inline edit / item buttons / drag to sort), `input`, `markdown`.
// Lives in the lazy widgets chunk. Everything a plugin sends is validated again here; admins get the controls, visitors a read-only view.
import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks'
import { Icon } from './icons'
import { LUCIDE } from './lucide'
import { t } from './i18n'
import { renderMarkdown } from './md'
import type { Block, ListItem } from './types'
import { track } from './wtrack'

// ---------- safe value helpers (the backend sanitizes too; this is defence in depth) ----------
export const str = (v: unknown): string => (typeof v === 'string' ? v : typeof v === 'number' && Number.isFinite(v) ? String(v) : '')
export const httpUrl = (u: unknown): string | null => (typeof u === 'string' && /^https?:\/\//i.test(u.trim()) ? u.trim() : null)
export const imgSrc = (u: unknown): string | null => (typeof u === 'string' && /^(https?:\/\/|data:image\/)/i.test(u.trim()) ? u.trim() : null)
export const arr = <T,>(v: unknown, max: number): T[] => (Array.isArray(v) ? (v.slice(0, max) as T[]) : [])
export const wIcon = (n?: unknown) => (typeof n === 'string' && Object.hasOwn(LUCIDE, n) ? n : '')

export interface Ctx {
  /** POSTs the action. `quiet`: toast only what the plugin returns (interactive controls); resolves to false when it failed (the error is toasted). */
  run: (action: string, params: unknown, o?: { quiet?: boolean }) => Promise<boolean>
  admin: boolean
  /** the page is in layout edit mode: the cards are dragged around, the controls inside are inert */
  editing: boolean
}

const ACTION_RE = /^[A-Za-z0-9_.:-]{1,64}$/
const okAction = (a: unknown): a is string => typeof a === 'string' && ACTION_RE.test(a)
const obj = (v: unknown): Record<string, unknown> => (v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : {})
const ID_RE = /^[A-Za-z0-9_.:-]{1,64}$/

type ListBlockT = Extract<Block, { type: 'list' }>
const interactive = (b: ListBlockT) => !!b.sortAction || (Array.isArray(b.items) && b.items.some((it) => it && (it.check || it.edit || it.actions)))

/**
 * An optimistic value that is shown until the real view takes over. `dep` is what the response changes (the block / its text): the
 * value is dropped when it changes, except while another request of ours is still in flight, so quick successive edits do not
 * flash back to the older state when the first response arrives.
 */
function useOptimistic<T>(dep: unknown) {
  const [v, setV] = useState<T | null>(null)
  const pending = useRef(0)
  const changed = useRef(false)
  useEffect(() => {
    if (pending.current) changed.current = true
    else setV(null)
  }, [dep])
  const apply = async (next: T, req: () => Promise<boolean>) => {
    pending.current++
    setV(next)
    const ok = await req()
    pending.current--
    if (!pending.current) {
      if (!ok || changed.current) setV(null)
      changed.current = false
    }
    return ok
  }
  return [v, apply] as const
}

// ---------- list ----------
/** Previews a drag with CSS `order` only (no DOM moves, so focus survives); `commit(ids)` runs on release. */
function beginListSort(e: PointerEvent, list: HTMLElement, row: HTMLElement, ids: string[], commit: (ids: string[]) => void) {
  const id = row.dataset.itemId!
  let cur = ids.slice()
  let ghost: HTMLElement | null = null
  let offX = 0
  let offY = 0
  const rows = () => Array.from(list.querySelectorAll<HTMLElement>(':scope > [data-item-id]'))
  const setOrder = () => {
    for (const r of rows()) r.style.order = String(cur.indexOf(r.dataset.itemId!) + 1)
  }
  track(e, {
    touchDelay: 260,
    threshold: 5,
    onStart: (sx, sy) => {
      const r = row.getBoundingClientRect()
      offX = sx - r.left
      offY = sy - r.top
      ghost = row.cloneNode(true) as HTMLElement
      ghost.removeAttribute('data-testid')
      ghost.querySelectorAll('[data-testid]').forEach((n) => n.removeAttribute('data-testid'))
      ghost.classList.add('dnd-ghost', 'wg-li-ghost')
      ghost.style.width = r.width + 'px'
      document.body.appendChild(ghost)
      row.dataset.ph = '1'
      list.dataset.sorting = '1'
      setOrder()
    },
    onMove: (x, y) => {
      if (!ghost) return
      ghost.style.transform = `translate(${x - offX}px,${y - offY}px)`
      const others = rows().filter((r) => r !== row)
      let at = 0
      for (const r of others) {
        const b = r.getBoundingClientRect()
        if (b.top + b.height / 2 < y) at++
      }
      const rest = cur.filter((k) => k !== id)
      const next = rest.slice()
      next.splice(at, 0, id)
      if (next.join() !== cur.join()) {
        cur = next
        setOrder()
      }
    },
    onEnd: (ok) => {
      ghost?.remove()
      delete row.dataset.ph
      delete list.dataset.sorting
      if (ok && cur.join() !== ids.join()) commit(cur)
      // the state update re-renders in the new order first; only then the preview order goes away
      requestAnimationFrame(() => {
        for (const r of rows()) r.style.order = ''
      })
    },
  })
}

export function ListBlock({ b, ctx }: { b: ListBlockT; ctx: Ctx }) {
  const items = arr<ListItem>(b.items, interactive(b) ? 200 : 20).filter((it) => it && typeof it === 'object')
  const [order, optimisticOrder] = useOptimistic<string[]>(b) // a fresh view (or a refetch) is the truth again
  const [live, setLive] = useState('')
  const list = useRef<HTMLDivElement>(null)
  const ids = items.map((it) => (typeof it.id === 'string' && ID_RE.test(it.id) ? it.id : ''))
  const sortable = ctx.admin && !ctx.editing && okAction(b.sortAction) && items.length > 1 && ids.every(Boolean) && new Set(ids).size === ids.length
  let shown = items
  if (order && sortable) {
    const by = new Map(items.map((it) => [it.id as string, it]))
    const o = order.map((k) => by.get(k)).filter(Boolean) as ListItem[]
    if (o.length === items.length) shown = o
  }
  const shownIds = shown.map((it) => it.id as string)
  const cur = useRef<string[]>(shownIds) // the order right now, ahead of the next render while a move is in flight
  cur.current = shownIds
  const commit = (next: string[]) => {
    cur.current = next
    return optimisticOrder(next, () => ctx.run(b.sortAction!, { ids: next }, { quiet: true }))
  }
  const keyMove = (e: KeyboardEvent, id: string) => {
    if (!sortable || !e.altKey || (e.key !== 'ArrowUp' && e.key !== 'ArrowDown')) return
    e.preventDefault()
    const ids = cur.current
    const i = ids.indexOf(id)
    const j = i + (e.key === 'ArrowUp' ? -1 : 1)
    if (i < 0 || j < 0 || j >= ids.length) return
    const next = ids.slice()
    next.splice(i, 1)
    next.splice(j, 0, id)
    setLive(t('widget.moved', { n: j + 1, total: next.length }))
    const active = document.activeElement as HTMLElement | null
    commit(next)
    requestAnimationFrame(() => {
      if (active && active.isConnected) active.focus()
      else list.current?.querySelector<HTMLElement>(`[data-item-id="${id}"]`)?.focus()
    })
  }
  const press = (e: PointerEvent, id: string) => {
    if (!sortable || !list.current) return
    const tg = e.target as HTMLElement
    if (tg.closest('button,input,textarea,select,a[href],[data-nodrag]')) return
    e.stopPropagation() // not the card's own long-press menu
    beginListSort(e, list.current, e.currentTarget as HTMLElement, shownIds, commit)
    void id
  }
  if (!items.length) {
    return b.empty ? (
      <div class="wg-list-empty" data-testid="widget-list-empty">
        {str(b.empty)}
      </div>
    ) : (
      <div class="wg-list" data-testid="widget-list" />
    )
  }
  return (
    <div class="wg-list" data-testid="widget-list" data-sortable={sortable ? '1' : undefined} ref={list}>
      {shown.map((it, i) => (
        <ListRow key={it.id || i} it={it} ctx={ctx} sortable={sortable} onPress={(e) => press(e, it.id as string)} onKey={(e) => keyMove(e, it.id as string)} />
      ))}
      {sortable && (
        <span class="vh" aria-live="polite">
          {live}
        </span>
      )}
    </div>
  )
}

/** A text input that takes the focus (and selects its text) when it appears. */
function FocusInput(p: { class: string; testid: string; value: string; maxLength: number; label: string; onInput: (v: string) => void; onKeyDown: (e: KeyboardEvent) => void; onBlur: () => void }) {
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => {
    ref.current?.focus()
    ref.current?.select()
  }, [])
  return <input ref={ref} type="text" class={p.class} data-testid={p.testid} value={p.value} maxLength={p.maxLength} aria-label={p.label} onInput={(e) => p.onInput((e.target as HTMLInputElement).value)} onKeyDown={p.onKeyDown} onBlur={p.onBlur} />
}

function ListRow({ it, ctx, sortable, onPress, onKey }: { it: ListItem; ctx: Ctx; sortable: boolean; onPress: (e: PointerEvent) => void; onKey: (e: KeyboardEvent) => void }) {
  const href = httpUrl(it.url)
  const img = imgSrc(it.image)
  const ic = wIcon(it.icon)
  const check = it.check && okAction(it.check.action) ? it.check : null
  const edit = ctx.admin && it.edit && okAction(it.edit.action) ? it.edit : null
  const acts = ctx.admin ? arr<NonNullable<ListItem['actions']>[number]>(it.actions, 3).filter((a) => a && okAction(a.action) && a.icon && a.label) : []
  const [ck, setCk] = useState<boolean | null>(null) // optimistic check state
  const [name, setName] = useState<string | null>(null) // optimistic title
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const done = useRef(false)
  useEffect(() => setCk(null), [check?.checked])
  useEffect(() => setName(null), [it.title])
  const checked = ck ?? !!check?.checked
  const title = name ?? str(it.title)

  if (!check && !edit && !acts.length && !sortable) {
    // plain row (the original list item): the whole row is the link
    const inner = (
      <>
        {img ? <img class="wg-li-img" src={img} alt="" loading="lazy" referrerpolicy="no-referrer" draggable={false} /> : ic ? <span class="wg-li-ico"><Icon n={ic} size={15} /></span> : null}
        <span class="wg-li-main">
          <span class="wg-li-t">{str(it.title)}</span>
          {it.subtitle && <span class="wg-li-s">{str(it.subtitle)}</span>}
        </span>
        {it.badge && <span class="wg-badge">{str(it.badge)}</span>}
        {it.right && <span class="wg-li-r">{str(it.right)}</span>}
      </>
    )
    return href ? (
      <a class="wg-li" href={href} target="_blank" rel="noopener noreferrer" data-testid="widget-list-item">
        {inner}
      </a>
    ) : (
      <div class="wg-li" data-testid="widget-list-item">
        {inner}
      </div>
    )
  }

  const startEdit = () => {
    if (!edit || editing) return
    done.current = false
    setDraft(title)
    setEditing(true)
  }
  const saveEdit = async () => {
    if (done.current || !edit) return
    done.current = true
    setEditing(false)
    const v = draft.trim()
    if (!v || v === title) return
    setName(v)
    const ok = await ctx.run(edit.action, { ...obj(edit.params), value: v }, { quiet: true })
    if (!ok) setName(null)
  }
  const toggle = async () => {
    if (!check || !ctx.admin) return
    const next = !checked
    setCk(next)
    const ok = await ctx.run(check.action, { ...obj(check.params), checked: next }, { quiet: true })
    if (!ok) setCk(null)
  }
  return (
    <div
      class="wg-li wg-li-x"
      data-testid="widget-list-item"
      data-item-id={it.id}
      data-checked={check ? (checked ? '1' : '0') : undefined}
      data-sort={sortable ? '1' : undefined}
      tabIndex={sortable ? 0 : undefined}
      onPointerDown={onPress as any}
      onKeyDown={onKey as any}
      onContextMenu={sortable ? (e) => (e.preventDefault(), e.stopPropagation()) : undefined}
    >
      {check && <input type="checkbox" class="wg-check" data-testid="widget-check" checked={checked} disabled={!ctx.admin} aria-label={title} onChange={toggle} />}
      {img ? <img class="wg-li-img" src={img} alt="" loading="lazy" referrerpolicy="no-referrer" draggable={false} /> : ic ? <span class="wg-li-ico"><Icon n={ic} size={15} /></span> : null}
      {editing ? (
        <FocusInput
          class="wg-li-edit"
          testid="widget-item-edit"
          value={draft}
          maxLength={500}
          label={t('widget.rename')}
          onInput={setDraft}
          onKeyDown={(e) => {
            e.stopPropagation()
            if (e.key === 'Enter') (e.preventDefault(), saveEdit())
            else if (e.key === 'Escape') (e.preventDefault(), (done.current = true), setEditing(false))
          }}
          onBlur={saveEdit}
        />
      ) : (
        <span class="wg-li-main">
          <span class="wg-li-t" data-testid="widget-item-title" data-editable={edit ? '1' : undefined} onDblClick={startEdit} onClick={ctx.editing ? startEdit : undefined}>
            {href ? (
              <a href={href} target="_blank" rel="noopener noreferrer">
                {title}
              </a>
            ) : (
              title
            )}
          </span>
          {it.subtitle && <span class="wg-li-s">{str(it.subtitle)}</span>}
        </span>
      )}
      {it.badge && <span class="wg-badge">{str(it.badge)}</span>}
      {it.right && <span class="wg-li-r">{str(it.right)}</span>}
      {acts.length > 0 && !editing && (
        <span class="wg-li-acts">
          {acts.map((a, k) => (
            <button
              key={k}
              type="button"
              class={'wg-li-act' + (/^(trash|x|eraser)/.test(a.icon) ? ' danger' : '')}
              data-testid="widget-item-action"
              data-action={a.action}
              aria-label={str(a.label)}
              title={str(a.label)}
              onClick={() => ctx.run(a.action, a.params, { quiet: true })}
            >
              <Icon n={wIcon(a.icon) || 'ellipsis'} size={14} />
            </button>
          ))}
        </span>
      )}
    </div>
  )
}

// ---------- input ----------
export function InputBlock({ b, ctx }: { b: Extract<Block, { type: 'input' }>; ctx: Ctx }) {
  const [v, setV] = useState(() => str(b.value))
  const [busy, setBusy] = useState(false)
  const ref = useRef<HTMLInputElement>(null)
  const refocus = useRef(false)
  useEffect(() => {
    if (!busy && refocus.current) {
      refocus.current = false
      ref.current?.focus()
    }
  }, [busy])
  if (!ctx.admin || !okAction(b.action)) return null
  const max = Math.min(20000, Math.max(1, Math.round(Number(b.maxLength)) || 500))
  const ic = wIcon(b.icon)
  const label = str(b.submit) || t('widget.input.submit')
  const submit = async (e: Event) => {
    e.preventDefault()
    const val = v
    if (!val.trim() || busy) return
    // optimistic: the field empties at once, and gets its text back if the action fails
    setV('')
    setBusy(true)
    const ok = await ctx.run(b.action, { ...obj(b.params), value: val }, { quiet: true })
    if (!ok) setV(val)
    refocus.current = true
    setBusy(false)
  }
  return (
    <form class="wg-input" data-testid="widget-input-form" aria-busy={busy} onSubmit={submit}>
      <input
        ref={ref}
        type="text"
        class="wg-input-f"
        data-testid="widget-input"
        value={v}
        maxLength={max}
        placeholder={str(b.placeholder)}
        aria-label={str(b.placeholder) || label}
        disabled={busy}
        autocomplete="off"
        onInput={(e) => setV((e.target as HTMLInputElement).value)}
      />
      <button type="submit" class="btn sm wg-input-b" data-testid="widget-input-submit" disabled={busy || !v.trim()} aria-label={label} title={label}>
        <Icon n={ic || 'plus'} size={15} />
        {!ic && str(b.submit) ? <span>{str(b.submit)}</span> : null}
      </button>
    </form>
  )
}

// ---------- markdown ----------
export function MarkdownBlock({ b, ctx }: { b: Extract<Block, { type: 'markdown' }>; ctx: Ctx }) {
  const src = str(b.text).slice(0, 20000)
  const [local, optimisticText] = useOptimistic<string>(b.text) // optimistic text
  const latest = useRef('')
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const ta = useRef<HTMLTextAreaElement>(null)
  const settled = useRef(false)
  const text = (local ?? src).replace(/\r\n?/g, '\n')
  latest.current = text
  const can = ctx.admin && okAction(b.editAction)
  const canEdit = can && !ctx.editing
  const save = (next: string): Promise<boolean> => {
    latest.current = next // a second toggle before the next render builds on this one
    return optimisticText(next, () => ctx.run(b.editAction!, { ...obj(b.params), value: next }, { quiet: true }))
  }
  const start = () => {
    if (!canEdit || editing) return
    settled.current = false
    setDraft(text)
    setEditing(true)
  }
  const finish = async (commit: boolean) => {
    if (settled.current) return
    settled.current = true
    setEditing(false)
    if (!commit || draft === text) return
    if (!(await save(draft))) {
      // keep the text: reopen the editor with it
      settled.current = false
      setDraft(draft)
      setEditing(true)
    }
  }
  const toggle = (line: number) => {
    const ls = latest.current.split('\n')
    const re = /^((?:\s{0,3}>)*\s*(?:[-*+]|\d{1,9}[.)])\s+\[)([ xX])(\])/
    if (!re.test(ls[line] ?? '')) return
    ls[line] = ls[line].replace(re, (_m, a: string, c: string, d: string) => a + (c === ' ' ? 'x' : ' ') + d)
    save(ls.join('\n'))
  }
  useLayoutEffect(() => {
    const el = ta.current
    if (!editing || !el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight + 2, Math.max(160, window.innerHeight * 0.7)) + 'px'
  }, [editing, draft])
  useEffect(() => {
    if (editing && ta.current) {
      ta.current.focus()
      ta.current.setSelectionRange(ta.current.value.length, ta.current.value.length)
    }
  }, [editing])

  if (editing) {
    return (
      <div class="wg-md-wrap" data-editing="1">
        <textarea
          ref={ta}
          class="wg-md-edit"
          data-testid="widget-markdown-input"
          value={draft}
          maxLength={20000}
          placeholder={str(b.placeholder)}
          aria-label={t('widget.md.edit')}
          rows={3}
          onInput={(e) => setDraft((e.target as HTMLTextAreaElement).value)}
          onKeyDown={(e) => {
            e.stopPropagation()
            if (e.key === 'Escape') {
              e.preventDefault()
              finish(false)
            } else if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
              e.preventDefault()
              finish(true)
            }
          }}
          onBlur={() => finish(true)}
        />
        <span class="wg-md-hint">{t('widget.md.hint')}</span>
      </div>
    )
  }
  const hint = !text.trim() ? str(b.placeholder) || (canEdit ? t('widget.md.empty') : '') : ''
  return (
    <div class="wg-md-wrap" data-editable={can ? '1' : undefined}>
      <div class="wg-md" data-testid="widget-markdown" onDblClick={(e) => !(e.target as HTMLElement).closest('input,a') && start()}>
        {text.trim() ? renderMarkdown(text, { widget: true, onToggle: can && !ctx.editing ? toggle : undefined }) : hint ? <p class="wg-md-ph" data-testid="widget-markdown-placeholder">{hint}</p> : null}
      </div>
      {canEdit && (
        <button type="button" class="wg-md-editbtn" data-testid="widget-markdown-edit" aria-label={t('widget.md.edit')} title={t('widget.md.edit')} onClick={start}>
          <Icon n="pencil" size={13} />
        </button>
      )}
    </div>
  )
}
