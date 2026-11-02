import type { ComponentChildren, Ref } from 'preact'
import { useLayoutEffect, useRef, useState } from 'preact/hooks'

export interface TriggerProps {
  ref: Ref<HTMLButtonElement>
  'aria-haspopup': 'menu'
  'aria-expanded': boolean
  'aria-controls': string
  onClick: () => void
}

const ITEMS = '[role^="menuitem"]:not([disabled])'

/**
 * Glass dropdown menu: closes on outside press, Esc, Tab and selection; ArrowUp/Down/Home/End move
 * between `[role^=menuitem]` elements; focus goes to the checked (else first) item on open and back
 * to the trigger on close.
 */
export function Dropdown(props: {
  id: string
  align: 'left' | 'right'
  label: string
  testid: string
  trigger: (p: TriggerProps, open: boolean) => ComponentChildren
  children: (close: (refocus?: boolean) => void) => ComponentChildren
  class?: string
}) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const btn = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const close = (refocus = true) => {
    setOpen(false)
    if (refocus) btn.current?.focus()
  }
  const items = () => Array.from(menu.current?.querySelectorAll<HTMLElement>(ITEMS) || [])

  useLayoutEffect(() => {
    if (!open) return
    const list = items()
    ;(list.find((x) => x.getAttribute('aria-checked') === 'true') || list[0])?.focus()
    const down = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        close()
      }
    }
    document.addEventListener('pointerdown', down, true)
    document.addEventListener('keydown', key, true)
    return () => {
      document.removeEventListener('pointerdown', down, true)
      document.removeEventListener('keydown', key, true)
    }
  }, [open])

  const onKeyDown = (e: KeyboardEvent) => {
    const k = e.key
    if (!open) {
      if (k === 'ArrowDown' && e.target === btn.current) (e.preventDefault(), setOpen(true))
      return
    }
    if (k === 'Tab') return setOpen(false)
    const list = items()
    if (!list.length) return
    const i = list.indexOf(document.activeElement as HTMLElement)
    let n = -1
    if (k === 'ArrowDown') n = (i + 1) % list.length
    else if (k === 'ArrowUp') n = i <= 0 ? list.length - 1 : i - 1
    else if (k === 'Home') n = 0
    else if (k === 'End') n = list.length - 1
    if (n < 0) return
    e.preventDefault()
    list[n].focus()
  }

  return (
    <div class={'dd ' + (props.class || '')} ref={root} onKeyDown={onKeyDown}>
      {props.trigger(
        { ref: btn, 'aria-haspopup': 'menu', 'aria-expanded': open, 'aria-controls': props.id, onClick: () => (open ? close() : setOpen(true)) },
        open,
      )}
      {open && (
        <div class={'menu glass menu-' + props.align} id={props.id} role="menu" aria-label={props.label} data-testid={props.testid} ref={menu}>
          {props.children(close)}
        </div>
      )}
    </div>
  )
}
