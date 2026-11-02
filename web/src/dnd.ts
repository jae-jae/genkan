// Minimal pointer-events drag & drop for links / categories.
// Items carry data-dnd-kind + data-dnd-item=<id>; link containers carry data-dnd-container=<key>.

export type DndKind = 'link' | 'category'
export interface DropResult {
  containerKey: string
  index: number
  /** link kind only: ordered ids of every container (dragged id excluded) */
  containers: Record<string, string[]>
}
interface Opts {
  kind: DndKind
  id: string
  el: HTMLElement
  onDrop: (r: DropResult) => void
  onStart?: () => void
}

const q = (root: ParentNode, sel: string) => Array.from(root.querySelectorAll<HTMLElement>(sel))

function rectDist(r: DOMRect, x: number, y: number) {
  const dx = Math.max(r.left - x, 0, x - r.right)
  const dy = Math.max(r.top - y, 0, y - r.bottom)
  return Math.hypot(dx, dy)
}

export function beginDrag(e: PointerEvent, o: Opts) {
  if (e.pointerType === 'mouse' && e.button !== 0) return
  const tgt = e.target as HTMLElement
  if (!tgt.closest('[data-handle]') && tgt.closest('button,input,select,textarea,a[href],[data-nodrag]')) return
  const touch = e.pointerType !== 'mouse'
  const sx = e.clientX
  const sy = e.clientY
  let lx = sx
  let ly = sy
  let started = false
  let timer = 0
  let raf = 0
  let ghost: HTMLElement | null = null
  let line: HTMLElement | null = null
  let hot: HTMLElement | null = null
  let offX = 0
  let offY = 0
  let target: { key: string; index: number } | null = null

  const blockTouch = (ev: TouchEvent) => {
    if (started && ev.cancelable) ev.preventDefault()
  }

  // a pinned link is rendered twice (pinned row + category): dim / ignore both copies
  const twins = () => (o.kind === 'link' ? q(document, `[data-dnd-kind="link"][data-dnd-item="${o.id}"]`) : [o.el])

  function place(x: number, y: number, w: number, h: number) {
    if (!line) return
    line.style.cssText = `left:${x}px;top:${y}px;width:${w}px;height:${h}px;display:block`
  }

  function compute() {
    const doc = document
    target = null
    if (hot) hot.classList.remove('drop-hot')
    hot = null
    const dragged = o.el
    if (o.kind === 'link') {
      const conts = q(doc, '[data-dnd-container]')
      let best: HTMLElement | null = null
      let bd = 1e9
      for (const c of conts) {
        const r = c.getBoundingClientRect()
        const pad = 16
        if (lx >= r.left - pad && lx <= r.right + pad && ly >= r.top - pad && ly <= r.bottom + pad) {
          const d = rectDist(r, lx, ly)
          if (d < bd) {
            bd = d
            best = c
          }
        }
      }
      if (!best) {
        if (line) line.style.display = 'none'
        return
      }
      best.classList.add('drop-hot')
      hot = best
      const items = q(best, '[data-dnd-kind="link"][data-dnd-item]').filter((i) => i.dataset.dndItem !== o.id)
      const key = best.dataset.dndContainer!
      if (!items.length) {
        const flow = best.querySelector<HTMLElement>('[data-dnd-flow]') || best
        const r = flow.getBoundingClientRect()
        target = { key, index: 0 }
        place(r.left, r.top, 3, 44)
        return
      }
      let ni = 0
      let nd = 1e9
      let nc = 1e9
      items.forEach((it, i) => {
        const r = it.getBoundingClientRect()
        const d = rectDist(r, lx, ly)
        const cd = Math.hypot(r.left + r.width / 2 - lx, r.top + r.height / 2 - ly)
        if (d < nd - 0.5 || (Math.abs(d - nd) <= 0.5 && cd < nc)) {
          ni = i
          nd = d
          nc = cd
        }
      })
      const r = items[ni].getBoundingClientRect()
      const before = lx < r.left + r.width / 2
      target = { key, index: ni + (before ? 0 : 1) }
      place(before ? r.left - 7 : r.right + 5, r.top, 3, r.height)
    } else {
      const items = q(doc, `[data-dnd-kind="${o.kind}"][data-dnd-item]`).filter((i) => i !== dragged)
      let idx = 0
      const rects = items.map((it) => it.getBoundingClientRect())
      // categories may sit in a multi-column grid: then compare in row-major order
      const multi = rects.some((r) => Math.abs(r.left - rects[0].left) > 20)
      for (const r of rects) {
        if (multi && ly >= r.top && ly <= r.bottom) {
          if (r.left + r.width / 2 < lx) idx++
        } else if (r.top + r.height / 2 < ly) idx++
      }
      target = { key: o.kind, index: idx }
      if (!items.length) return
      if (idx < items.length) {
        const r = items[idx].getBoundingClientRect()
        place(r.left, r.top - 9, r.width, 3)
      } else {
        const r = items[items.length - 1].getBoundingClientRect()
        place(r.left, r.bottom + 6, r.width, 3)
      }
    }
  }

  function begin() {
    started = true
    o.onStart?.()
    const r = o.el.getBoundingClientRect()
    offX = sx - r.left
    offY = sy - r.top
    ghost = o.el.cloneNode(true) as HTMLElement
    ghost.removeAttribute('data-testid')
    ghost.removeAttribute('data-dnd-item')
    ghost.classList.add('dnd-ghost')
    ghost.style.width = r.width + 'px'
    ghost.style.height = r.height + 'px'
    document.body.appendChild(ghost)
    line = document.createElement('div')
    line.className = 'dnd-line'
    document.body.appendChild(line)
    twins().forEach((x) => x.classList.add('dnd-dim'))
    document.documentElement.classList.add('is-dragging')
    try {
      o.el.setPointerCapture(e.pointerId)
    } catch {}
    loop()
  }

  function update() {
    if (!ghost) return
    ghost.style.transform = `translate(${lx - offX}px,${ly - offY}px) scale(1.04)`
    compute()
  }

  function loop() {
    const H = window.innerHeight
    let vy = 0
    if (ly < 80) vy = -(80 - ly) / 4
    else if (ly > H - 80) vy = (ly - (H - 80)) / 4
    if (vy) window.scrollBy(0, vy)
    update()
    raf = requestAnimationFrame(loop)
  }

  function cleanup() {
    clearTimeout(timer)
    cancelAnimationFrame(raf)
    document.removeEventListener('pointermove', move)
    document.removeEventListener('pointerup', up)
    document.removeEventListener('pointercancel', cancel)
    document.removeEventListener('touchmove', blockTouch)
    document.removeEventListener('keydown', key)
    ghost?.remove()
    line?.remove()
    hot?.classList.remove('drop-hot')
    twins().forEach((x) => x.classList.remove('dnd-dim'))
    document.documentElement.classList.remove('is-dragging')
    try {
      o.el.releasePointerCapture(e.pointerId)
    } catch {}
  }

  function finish(commit: boolean) {
    if (started && commit) compute() // the last pointermove may not have been through a frame yet
    const t = target
    const was = started
    cleanup()
    if (was) {
      // swallow the click that follows a drag
      const swallow = (ev: Event) => {
        ev.stopPropagation()
        ev.preventDefault()
      }
      document.addEventListener('click', swallow, { capture: true, once: true })
      setTimeout(() => document.removeEventListener('click', swallow, true), 60)
    }
    if (!was || !commit || !t) return
    const containers: Record<string, string[]> = {}
    if (o.kind === 'link') {
      for (const c of q(document, '[data-dnd-container]'))
        containers[c.dataset.dndContainer!] = q(c, '[data-dnd-kind="link"][data-dnd-item]')
          .filter((i) => i.dataset.dndItem !== o.id)
          .map((i) => i.dataset.dndItem!)
    }
    o.onDrop({ containerKey: t.key, index: t.index, containers })
  }

  function move(ev: PointerEvent) {
    if (ev.pointerId !== e.pointerId) return
    lx = ev.clientX
    ly = ev.clientY
    if (!started) {
      const d = Math.hypot(lx - sx, ly - sy)
      if (touch) {
        if (d > 9) cleanup()
      } else if (d > 5) begin()
    }
  }
  const up = (ev: PointerEvent) => ev.pointerId === e.pointerId && finish(true)
  const cancel = (ev: PointerEvent) => ev.pointerId === e.pointerId && finish(false)
  const key = (ev: KeyboardEvent) => ev.key === 'Escape' && finish(false)

  document.addEventListener('pointermove', move)
  document.addEventListener('pointerup', up)
  document.addEventListener('pointercancel', cancel)
  document.addEventListener('keydown', key)
  document.addEventListener('touchmove', blockTouch, { passive: false })
  if (touch) timer = window.setTimeout(begin, 260)
}
