// Pointer tracking for widget drag & drop and resizing (same feel as dnd.ts: mouse starts after 5px, touch after a 260ms long press
// that is cancelled by moving > 9px first, edge auto-scroll, Esc cancels, the click that follows a drag is swallowed).
// Lives in the lazy widget chunk; dnd.ts (links / categories) is untouched.

export interface Track {
  /** touch: long-press before the gesture starts (0 = like the mouse, by `threshold`; the element must have touch-action: none) */
  touchDelay: number
  /** mouse movement (px) before the gesture starts */
  threshold: number
  /** element that receives pointer capture while tracking (optional) */
  capture?: HTMLElement
  onStart: (x: number, y: number) => void
  /** called once per frame while started (also while only the page auto-scrolls) */
  onMove: (x: number, y: number) => void
  onEnd: (commit: boolean) => void
}

export function track(e: PointerEvent, o: Track) {
  if (e.pointerType === 'mouse' && e.button !== 0) return
  const touch = e.pointerType !== 'mouse'
  const sx = e.clientX
  const sy = e.clientY
  let x = sx
  let y = sy
  let started = false
  let timer = 0
  let raf = 0

  const blockTouch = (ev: TouchEvent) => {
    if (started && ev.cancelable) ev.preventDefault()
  }
  function begin() {
    started = true
    document.documentElement.classList.add('is-dragging')
    try {
      o.capture?.setPointerCapture(e.pointerId)
    } catch {}
    o.onStart(sx, sy)
    loop()
  }
  function loop() {
    const H = window.innerHeight
    let vy = 0
    if (y < 80) vy = -(80 - y) / 4
    else if (y > H - 80) vy = (y - (H - 80)) / 4
    if (vy) window.scrollBy(0, vy)
    o.onMove(x, y)
    raf = requestAnimationFrame(loop)
  }
  function finish(commit: boolean) {
    clearTimeout(timer)
    cancelAnimationFrame(raf)
    document.removeEventListener('pointermove', move)
    document.removeEventListener('pointerup', up)
    document.removeEventListener('pointercancel', cancel)
    document.removeEventListener('touchmove', blockTouch)
    document.removeEventListener('keydown', key)
    if (!started) return
    if (commit) o.onMove(x, y) // the last pointermove may not have been through a frame yet
    document.documentElement.classList.remove('is-dragging')
    try {
      o.capture?.releasePointerCapture(e.pointerId)
    } catch {}
    const swallow = (ev: Event) => {
      ev.stopPropagation()
      ev.preventDefault()
    }
    document.addEventListener('click', swallow, { capture: true, once: true })
    setTimeout(() => document.removeEventListener('click', swallow, true), 60)
    o.onEnd(commit)
  }
  function move(ev: PointerEvent) {
    if (ev.pointerId !== e.pointerId) return
    x = ev.clientX
    y = ev.clientY
    if (started) return
    const d = Math.hypot(x - sx, y - sy)
    if (touch && o.touchDelay) {
      if (d > 9) finish(false)
    } else if (d > o.threshold) begin()
  }
  const up = (ev: PointerEvent) => ev.pointerId === e.pointerId && finish(true)
  const cancel = (ev: PointerEvent) => ev.pointerId === e.pointerId && finish(false)
  const key = (ev: KeyboardEvent) => ev.key === 'Escape' && finish(false)

  document.addEventListener('pointermove', move)
  document.addEventListener('pointerup', up)
  document.addEventListener('pointercancel', cancel)
  document.addEventListener('keydown', key)
  document.addEventListener('touchmove', blockTouch, { passive: false })
  if (touch && o.touchDelay) timer = window.setTimeout(begin, o.touchDelay)
}
