import type { ComponentChildren } from 'preact'
import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks'
import { Icon } from './icons'
import { BUILTIN_ICONS } from './lucide'
import { t } from './i18n'
import { dismissToast, toasts } from './store'
import { firstChar, hueOf, parseIcon } from './util'

export function LinkIcon({ icon, title, size = 24 }: { icon: string; title: string; size?: number }) {
  const p = parseIcon(icon)
  const [bad, setBad] = useState(false)
  useEffect(() => setBad(false), [icon])
  const style = { '--s': size + 'px' } as any
  if (p.kind === 'emoji' && p.value) return <span class="ico ico-emoji" style={style}>{p.value}</span>
  if (p.kind === 'lucide')
    return (
      <span class="ico ico-lucide" style={{ ...style, '--h': hueOf(title || '?') }}>
        <Icon n={p.value} size={Math.round(size * 0.58)} />
      </span>
    )
  if (p.kind === 'img' && !bad)
    return <img class="ico" style={style} src={p.value} alt="" loading="lazy" draggable={false} onError={() => setBad(true)} />
  return (
    <span class="ico ico-letter" style={{ ...style, '--h': hueOf(title || '?') }}>
      {firstChar(title)}
    </span>
  )
}

/**
 * Searchable grid of the built-in Lucide icons, shared by the workspace and link editors.
 * `value` is `lucide:<name>` (or ''); `testid` prefixes the grid, its search box (`-search`) and its cells (`-<name>`).
 */
export function IconPicker({ value, onPick, testid, label }: { value: string; onPick: (v: string) => void; testid: string; label?: string }) {
  const [q, setQ] = useState('')
  const needle = q.trim().toLowerCase().replace(/\s+/g, '-')
  const shown = BUILTIN_ICONS.filter((n) => !needle || n.includes(needle))
  const lid = testid + '-label'
  const grid = useRef<HTMLDivElement>(null)
  // open with the current choice in view (the grid scrolls on small screens)
  useEffect(() => {
    const g = grid.current
    const sel = g?.querySelector<HTMLElement>('[aria-checked="true"]')
    if (g && sel) g.scrollTop += sel.getBoundingClientRect().top - g.getBoundingClientRect().top - (g.clientHeight - sel.offsetHeight) / 2
  }, [])
  return (
    <div class="icon-picker">
      <div class="field-line">
        {label && <label id={lid}>{label}</label>}
        <span class={'search-mini' + (label ? '' : ' grow')}>
          <Icon n="search" size={14} />
          <input type="text" data-testid={testid + '-search'} aria-label={t('ws.iconSearch')} placeholder={t('ws.iconSearch')} value={q} onInput={(e) => setQ((e.target as HTMLInputElement).value)} onKeyDown={(e) => e.key === 'Enter' && e.preventDefault()} />
        </span>
      </div>
      <div class="icon-grid" ref={grid} role="radiogroup" aria-label={label ? undefined : t('link.icon')} aria-labelledby={label ? lid : undefined} data-testid={testid} data-value={value}>
        {shown.map((n) => (
          <button type="button" key={n} class="icon-cell" role="radio" aria-checked={value === 'lucide:' + n} aria-label={n} title={n} data-testid={testid + '-' + n} onClick={() => onPick('lucide:' + n)}>
            <Icon n={n} size={19} />
          </button>
        ))}
        {!shown.length && <span class="hint">{t('ws.iconNone')}</span>}
      </div>
    </div>
  )
}

export function Toasts() {
  return (
    <div class="toasts" data-testid="toast-container" aria-live="polite">
      {toasts.value.map((x) => (
        <div key={x.id} class="toast" data-kind={x.kind} data-testid="toast" role="status" onClick={() => dismissToast(x.id)}>
          {x.msg}
        </div>
      ))}
    </div>
  )
}

const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]):not([type=hidden]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])'

export function Modal(props: {
  title: string
  onClose: () => void
  children: ComponentChildren
  footer?: ComponentChildren
  wide?: boolean
  cls?: string
  testid?: string
  focusFirst?: boolean
}) {
  const ref = useRef<HTMLDivElement>(null)
  const down = useRef(false)
  const close = useRef(props.onClose)
  close.current = props.onClose
  useLayoutEffect(() => {
    const prev = document.activeElement as HTMLElement | null
    document.documentElement.classList.add('modal-open')
    if (props.focusFirst !== false) {
      const f = ref.current?.querySelector<HTMLElement>('input:not([type=hidden]):not([type=file]):not([type=checkbox]),select,textarea')
      f?.focus()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        close.current()
      } else if (e.key === 'Tab' && ref.current) {
        const els = Array.from(ref.current.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((x) => x.offsetParent !== null)
        if (!els.length) return
        const first = els[0]
        const last = els[els.length - 1]
        if (e.shiftKey && document.activeElement === first) (e.preventDefault(), last.focus())
        else if (!e.shiftKey && document.activeElement === last) (e.preventDefault(), first.focus())
      }
    }
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.documentElement.classList.remove('modal-open')
      prev?.focus?.()
    }
  }, [])
  return (
    <div
      class="overlay"
      onPointerDown={(e) => (down.current = e.target === e.currentTarget)}
      onClick={(e) => down.current && e.target === e.currentTarget && props.onClose()}
    >
      <div
        class={'modal glass' + (props.wide ? ' modal-wide' : '') + (props.cls ? ' ' + props.cls : '')}
        role="dialog"
        aria-modal="true"
        aria-label={props.title}
        data-testid={props.testid || 'modal'}
        ref={ref}
      >
        <div class="modal-head">
          <h2>{props.title}</h2>
          <button class="icon-btn sm" aria-label={t('common.close')} data-testid="modal-close" onClick={props.onClose}>
            <Icon n="x" size={16} />
          </button>
        </div>
        <div class="modal-body">{props.children}</div>
        {props.footer && <div class="modal-foot">{props.footer}</div>}
      </div>
    </div>
  )
}

export function Switch({ checked, onChange, id, testid, label }: { checked: boolean; onChange: (v: boolean) => void; id?: string; testid?: string; label: string }) {
  return (
    <button
      type="button"
      id={id}
      role="switch"
      aria-checked={checked}
      aria-label={label}
      data-testid={testid}
      class="switch"
      onClick={() => onChange(!checked)}
    >
      <i />
    </button>
  )
}

export function Row(props: { label: string; hint?: string; children: ComponentChildren; for?: string }) {
  return (
    <div class="row">
      <label class="row-l" for={props.for}>
        <span>{props.label}</span>
        {props.hint && <small>{props.hint}</small>}
      </label>
      <div class="row-c">{props.children}</div>
    </div>
  )
}
