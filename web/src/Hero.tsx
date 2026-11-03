import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { get } from './api'
import { Icon } from './icons'
import { LUCIDE } from './lucide'
import { LinkIcon } from './parts'
import { lang, t } from './i18n'
import { categories, curWs, hasSearchPlugin, links, openLink, settings, toast, webSearch, workspaces } from './store'
import type { SearchGroup, SearchResult } from './types'
import { copyText, hostOf, searchLinks } from './util'

export function Clock() {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    let id = 0
    const tick = () => {
      const d = new Date()
      setNow(d)
      id = window.setTimeout(tick, 60000 - (d.getSeconds() * 1000 + d.getMilliseconds()) + 30)
    }
    id = window.setTimeout(tick, 60000 - (now.getSeconds() * 1000 + now.getMilliseconds()) + 30)
    return () => clearTimeout(id)
  }, [])
  const loc = lang.value === 'zh' ? 'zh-CN' : 'en-US'
  const time = now.toLocaleTimeString(loc, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
  const date = now.toLocaleDateString(loc, { weekday: 'long', month: 'long', day: 'numeric' })
  return (
    <div class="clock" data-testid="clock">
      <div class="clock-time">{time}</div>
      <div class="clock-date">{date}</div>
    </div>
  )
}

let inputEl: HTMLInputElement | null = null
export const focusSearch = () => {
  inputEl?.focus()
  inputEl?.select()
}
export const searchFocused = () => !!inputEl && document.activeElement === inputEl

interface PluginHit {
  g: SearchGroup
  r: SearchResult
}
const iconOf = (n?: string) => (n && LUCIDE[n] ? n : '')

/** Plugin search (docs/PLUGINS.md §7.4): debounced 250 ms; never requests anything while no enabled plugin provides search. */
function usePluginSearch(query: string) {
  const [groups, setGroups] = useState<SearchGroup[]>([])
  const seq = useRef(0)
  const on = hasSearchPlugin.value
  useEffect(() => {
    const my = ++seq.current
    if (!on || !query) {
      setGroups([])
      return
    }
    const id = window.setTimeout(async () => {
      let out: SearchGroup[] = []
      try {
        const r = await get<{ groups: SearchGroup[] | null }>('/api/plugins/search?q=' + encodeURIComponent(query), { 'Accept-Language': lang.value })
        out = (r?.groups || []).map((g) => ({ ...g, results: (g.results || []).filter((x) => x && typeof x.title === 'string').slice(0, 8) })).filter((g) => g.results!.length)
      } catch {}
      if (my === seq.current) setGroups(out)
    }, 250)
    return () => clearTimeout(id)
  }, [query, on])
  return groups
}

export function Search() {
  const [q, setQ0] = useState('')
  const [open, setOpen] = useState(false)
  const [sel, setSel] = useState(0)
  // reset the selection together with the query (an effect would run late and could undo a quick ArrowDown)
  const setQ = (v: string) => (setQ0(v), setSel(0))
  const cats = categories.value
  const lk = links.value
  const catMap = useMemo(() => new Map(cats.map((c) => [c.id, c])), [cats])
  const wsMap = useMemo(() => new Map(workspaces.value.map((w) => [w.id, w])), [workspaces.value])
  const hits = useMemo(() => (q.trim() ? searchLinks(q.trim(), lk, catMap).slice(0, 8) : []), [q, lk, catMap])
  const query = q.trim()
  const groups = usePluginSearch(query)
  const phits = useMemo<PluginHit[]>(() => groups.flatMap((g) => (g.results || []).map((r) => ({ g, r }))), [groups])
  const nl = hits.length
  const count = query ? nl + phits.length + 1 : 0
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    listRef.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [sel])

  const reset = () => {
    setQ('')
    setOpen(false)
    inputEl?.blur()
  }
  const activate = (i: number) => {
    if (i < nl) openLink(hits[i].link)
    else if (i < nl + phits.length) {
      const r = phits[i - nl].r
      if (r.url && /^https?:\/\//i.test(r.url)) {
        if (settings.value.open_in_new_tab) window.open(r.url, '_blank', 'noopener')
        else location.assign(r.url)
      } else if (r.copy) copyText(r.copy).then((ok) => toast(ok ? t('search.copied') : t('err.copy'), ok ? 'success' : 'error'))
      else return
    } else if (query) webSearch(query)
    reset()
  }
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'ArrowDown' && count) {
      e.preventDefault()
      setSel((sel + 1) % count)
    } else if (e.key === 'ArrowUp' && count) {
      e.preventDefault()
      setSel((sel - 1 + count) % count)
    } else if (e.key === 'Enter' && count && !e.isComposing) {
      e.preventDefault()
      activate(sel)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      if (q) setQ('')
      else reset()
    }
  }
  const showList = open && !!query
  return (
    <div class="search-wrap">
      <div class="search glass" data-open={showList ? '1' : undefined}>
        <span class="search-ico">
          <Icon n="search" size={20} />
        </span>
        <input
          ref={(el) => {
            inputEl = el
          }}
          data-testid="search-input"
          type="text"
          role="combobox"
          aria-expanded={showList}
          aria-controls="search-list"
          aria-activedescendant={showList ? 'sr-' + sel : undefined}
          aria-label={t('search.label')}
          placeholder={t('search.placeholder', { ws: curWs.value?.name || '' })}
          autocomplete="off"
          autocapitalize="off"
          spellcheck={false}
          enterkeyhint="search"
          value={q}
          onInput={(e) => {
            setQ((e.target as HTMLInputElement).value)
            setOpen(true)
          }}
          onFocus={() => setOpen(true)}
          onBlur={() => setTimeout(() => setOpen(false), 120)}
          onKeyDown={onKey}
        />
        {q ? (
          <button class="search-clear" aria-label={t('search.clear')} onClick={() => (setQ(''), inputEl?.focus())}>
            <Icon n="x" size={14} />
          </button>
        ) : (
          <kbd class="search-kbd">/</kbd>
        )}
      </div>
      {showList && (
        <div class="results glass" id="search-list" role="listbox" data-testid="search-results" ref={listRef} onMouseDown={(e) => e.preventDefault()}>
          {hits.map((h, i) => {
            const l = h.link
            const meta = [wsMap.get(l.workspace_id)?.name, catMap.get(l.category_id)?.name, hostOf(l.url)].filter(Boolean).join(' · ')
            return (
              <div
                key={l.id}
                id={'sr-' + i}
                class="result"
                role="option"
                aria-selected={sel === i}
                data-testid="search-result"
                data-link-id={l.id}
                onMouseMove={() => sel !== i && setSel(i)}
                onClick={() => activate(i)}
              >
                <LinkIcon icon={l.icon} title={l.title} size={26} />
                <span class="result-main">
                  <span class="result-title">{l.title}</span>
                  <span class="result-meta">{meta}</span>
                </span>
                {sel === i && <Icon n="corner-down-left" size={15} />}
              </div>
            )
          })}
          {groups.map((g) => (
            <div key={g.plugin} role="group" aria-label={g.name} data-testid="search-plugin-group" data-plugin={g.plugin}>
              <div class="result-group">
                <Icon n={iconOf(g.icon) || 'puzzle'} size={13} />
                <span>{g.name}</span>
              </div>
              {(g.results || []).map((r) => {
                const i = nl + phits.findIndex((h) => h.r === r)
                const copyOnly = !r.url && !!r.copy
                return (
                  <div
                    key={i}
                    id={'sr-' + i}
                    class="result"
                    role="option"
                    aria-selected={sel === i}
                    data-testid="plugin-search-result"
                    data-plugin={g.plugin}
                    data-copy={copyOnly ? '1' : undefined}
                    onMouseMove={() => sel !== i && setSel(i)}
                    onClick={() => activate(i)}
                  >
                    <span class="ico ico-web" style={{ '--s': '26px' } as any}>
                      <Icon n={iconOf(r.icon) || iconOf(g.icon) || (copyOnly ? 'copy' : 'puzzle')} size={15} />
                    </span>
                    <span class="result-main">
                      <span class="result-title">{r.title}</span>
                      {r.subtitle && <span class="result-meta">{r.subtitle}</span>}
                    </span>
                    {sel === i && <Icon n={copyOnly ? 'copy' : 'corner-down-left'} size={15} />}
                  </div>
                )
              })}
            </div>
          ))}
          <div
            id={'sr-' + (nl + phits.length)}
            class="result result-web"
            role="option"
            aria-selected={sel === nl + phits.length}
            data-testid="search-web"
            onMouseMove={() => sel !== nl + phits.length && setSel(nl + phits.length)}
            onClick={() => activate(nl + phits.length)}
          >
            <span class="ico ico-web" style={{ '--s': '26px' } as any}>
              <Icon n="globe" size={15} />
            </span>
            <span class="result-main">
              <span class="result-title">{t('search.web', { q: query })}</span>
              {!hits.length && !phits.length && <span class="result-meta">{t('search.none')}</span>}
            </span>
            {sel === nl + phits.length && <Icon n="corner-down-left" size={15} />}
          </div>
        </div>
      )}
    </div>
  )
}

export function Hero() {
  return (
    <section class="hero">
      {settings.value.show_clock && <Clock />}
      <Search />
    </section>
  )
}
