import { LUCIDE } from './lucide'

/** Lucide (ISC) icon by name; see lucide.ts. `filled` fills the shape (used for the active pin star). */
export function Icon({ n, size = 18, filled }: { n: string; size?: number; filled?: boolean }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill={filled ? 'currentColor' : 'none'}
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
      focusable="false"
      data-icon={n}
      dangerouslySetInnerHTML={{ __html: LUCIDE[n] || LUCIDE['layout-grid'] }}
    />
  )
}

export const wsIconName = (icon: string) => {
  const n = icon.startsWith('lucide:') ? icon.slice(7) : ''
  return n && LUCIDE[n] ? n : 'layout-grid'
}
/** Workspace icon: value is `lucide:<name>`; anything else (legacy emoji) falls back to layout-grid. */
export const WsIcon = ({ icon, size = 17 }: { icon: string; size?: number }) => <Icon n={wsIconName(icon)} size={size} />

export function Logo({ size = 56 }: { size?: number }) {
  return (
    <svg viewBox="0 0 64 64" width={size} height={size} aria-hidden="true">
      <defs>
        <linearGradient id="lg" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stop-color="#8b6cff" />
          <stop offset="1" stop-color="#19b5fe" />
        </linearGradient>
      </defs>
      <rect width="64" height="64" rx="16" fill="url(#lg)" />
      <g fill="none" stroke="#fff" stroke-width="4.5" stroke-linecap="round" stroke-linejoin="round">
        <path d="M13 22c13-5 25-5 38 0" />
        <path d="M19 31h26" />
        <path d="M22 24v21M42 24v21" />
      </g>
    </svg>
  )
}
