import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks'
import { Icon } from './icons'
import { t } from './i18n'
import {
  askDeleteLink,
  closeCtx,
  ctx,
  curWs,
  editMode,
  isAdmin,
  modal,
  openSettings,
  showLogin,
  togglePin,
  trackClick,
  toast,
  type CtxItem,
} from './store'
import type { Link } from './types'
import { copyText, safeHref } from './util'

let seq = 0
const isSheet = () => typeof matchMedia === 'function' && matchMedia('(max-width: 640px)').matches

export function openCtx(x: number, y: number, items: CtxItem[], testid: string, title?: string) {
  ctx.value = { key: ++seq, x, y, items, sheet: isSheet(), testid, title }
}

export function linkMenu(l: Link): CtxItem[] {
  const href = safeHref(l.url)
  const items: CtxItem[] = [
    {
      id: 'ctx-open',
      label: t('ctx.open'),
      icon: 'corner-down-left',
      onSelect: () => (trackClick(l), location.assign(href)),
    },
    {
      id: 'ctx-open-new',
      label: t('ctx.openNew'),
      icon: 'external-link',
      onSelect: () => (trackClick(l), window.open(href, '_blank', 'noopener')),
    },
    {
      id: 'ctx-copy-link',
      label: t('ctx.copyLink'),
      icon: 'link',
      onSelect: () => copyText(l.url).then((ok) => toast(ok ? t('toast.linkCopied') : t('err.copy'), ok ? 'success' : 'error')),
    },
  ]
  if (isAdmin.value)
    items.push(
      { id: 'sep1', label: '', sep: true, onSelect() {} },
      { id: 'ctx-edit', label: t('ctx.edit'), icon: 'pencil', onSelect: () => (modal.value = { kind: 'link', id: l.id }) },
      { id: 'ctx-pin', label: l.pinned ? t('ctx.unpin') : t('ctx.pin'), icon: l.pinned ? 'pin-off' : 'pin', onSelect: () => togglePin(l) },
      { id: 'ctx-delete', label: t('common.delete'), icon: 'trash-2', danger: true, onSelect: () => askDeleteLink(l) },
    )
  return items
}

export function pageMenu(): CtxItem[] {
  if (!isAdmin.value) return [{ id: 'ctx-login', label: t('top.login'), icon: 'log-in', onSelect: () => (showLogin.value = true) }]
  const ws = curWs.value
  return [
    {
      id: 'ctx-new-link',
      label: t('ctx.newLink'),
      icon: 'plus',
      onSelect: () => ws && (modal.value = { kind: 'link', defaults: { workspace_id: ws.id, category_id: '', pinned: false } }),
    },
    {
      id: 'ctx-new-category',
      label: t('ctx.newCategory'),
      icon: 'folder-plus',
      onSelect: () => ws && (modal.value = { kind: 'category', workspace_id: ws.id }),
    },
    { id: 'sep1', label: '', sep: true, onSelect() {} },
    { id: 'ctx-wallpaper', label: t('ctx.wallpaper'), icon: 'image', onSelect: () => openSettings('appearance') },
    { id: 'ctx-edit-mode', label: t('top.edit'), icon: 'pencil', checked: editMode.value, onSelect: () => (editMode.value = !editMode.value) },
    { id: 'ctx-ai', label: 'AI', icon: 'bot', onSelect: () => openSettings('ai') },
    { id: 'ctx-settings', label: t('top.settings'), icon: 'settings', onSelect: () => openSettings() },
  ]
}

/** Right-click handler for the page background; native menu is kept for inputs, selections and overlays. */
export function onPageContextMenu(e: MouseEvent) {
  if (e.defaultPrevented || !document.querySelector('.page')) return
  const tg = e.target as HTMLElement | null
  if (!tg || tg.closest('input,textarea,select,[contenteditable="true"],.overlay,.ctx,.menu,.results,.cap')) return
  if (String(window.getSelection() || '')) return
  e.preventDefault()
  openCtx(e.clientX, e.clientY, pageMenu(), 'ctx-menu-page')
}

export function CtxMenu() {
  const c = ctx.value
  return c ? <Inner key={c.key} c={c} /> : null
}

function Inner({ c }: { c: NonNullable<typeof ctx.value> }) {
  const ref = useRef<HTMLDivElement>(null)
  const armed = useRef(false) // a click only counts after a press that started on the menu (not the finger that opened it)
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null)

  useLayoutEffect(() => {
    const el = ref.current!
    if (!c.sheet) {
      const r = el.getBoundingClientRect()
      const m = 8
      setPos({ left: Math.max(m, Math.min(c.x, innerWidth - r.width - m)), top: Math.max(m, Math.min(c.y, innerHeight - r.height - m)) })
    }
    const prev = document.activeElement as HTMLElement | null
    const down = (ev: Event) => {
      if (!el.contains(ev.target as Node)) closeCtx()
    }
    const key = (ev: KeyboardEvent) => {
      if (ev.key === 'Escape') {
        ev.preventDefault()
        ev.stopPropagation()
        closeCtx()
        return
      }
      if (ev.key === 'Tab') return closeCtx()
      const list = Array.from(el.querySelectorAll<HTMLElement>('[role^="menuitem"]'))
      const i = list.indexOf(document.activeElement as HTMLElement)
      let n = -1
      if (ev.key === 'ArrowDown') n = (i + 1) % list.length
      else if (ev.key === 'ArrowUp') n = i <= 0 ? list.length - 1 : i - 1
      else if (ev.key === 'Home') n = 0
      else if (ev.key === 'End') n = list.length - 1
      if (n < 0) return
      ev.preventDefault()
      list[n].focus()
    }
    const away = () => closeCtx()
    document.addEventListener('pointerdown', down, true)
    document.addEventListener('contextmenu', down, true)
    document.addEventListener('keydown', key, true)
    window.addEventListener('scroll', away, true)
    window.addEventListener('wheel', away, { passive: true })
    window.addEventListener('resize', away)
    window.addEventListener('blur', away)
    return () => {
      document.removeEventListener('pointerdown', down, true)
      document.removeEventListener('contextmenu', down, true)
      document.removeEventListener('keydown', key, true)
      window.removeEventListener('scroll', away, true)
      window.removeEventListener('wheel', away)
      window.removeEventListener('resize', away)
      window.removeEventListener('blur', away)
      if (prev && prev !== document.body && document.contains(prev) && !modal.value) prev.focus({ preventScroll: true })
    }
  }, [])

  // focus the first item once the menu is placed (it is visibility:hidden until then); the touch sheet keeps focus where it was
  useEffect(() => {
    if (pos) ref.current?.querySelector<HTMLElement>('[role^="menuitem"]')?.focus({ preventScroll: true })
  }, [pos])

  const menu = (
    <div
      ref={ref}
      class={'ctx glass' + (c.sheet ? ' ctx-sheet' : '')}
      role="menu"
      aria-label={c.title || t('ctx.label')}
      data-testid={c.testid}
      onContextMenu={(e) => e.preventDefault()}
      onPointerDown={() => (armed.current = true)}
      style={c.sheet ? undefined : { left: (pos?.left ?? c.x) + 'px', top: (pos?.top ?? c.y) + 'px', visibility: pos ? 'visible' : 'hidden' }}
    >
      {c.sheet && (
        <>
          <div class="ctx-grab" aria-hidden="true" />
          {c.title && <div class="ctx-title">{c.title}</div>}
        </>
      )}
      {c.items.map((it, i) =>
        it.sep ? (
          <div key={i} class="mi-sep" role="separator" />
        ) : (
          <button
            key={it.id}
            class={'mi' + (it.danger ? ' danger' : '')}
            role={it.checked !== undefined ? 'menuitemcheckbox' : 'menuitem'}
            aria-checked={it.checked}
            data-testid={it.id}
            onClick={(e) => {
              if (e.detail !== 0 && !armed.current) return
              closeCtx()
              it.onSelect()
            }}
          >
            <span class="mi-ico">{it.icon && <Icon n={it.icon} size={16} />}</span>
            <span class="mi-name">{it.label}</span>
            {it.checked !== undefined && <span class="mi-check">{it.checked && <Icon n="check" size={15} />}</span>}
          </button>
        ),
      )}
    </div>
  )
  return c.sheet ? (
    <div class="ctx-overlay" onContextMenu={(e) => e.preventDefault()}>
      {menu}
    </div>
  ) : (
    menu
  )
}
