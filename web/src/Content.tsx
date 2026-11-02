import { useMemo, useState } from 'preact/hooks'
import { Icon, WsIcon } from './icons'
import { LinkIcon } from './parts'
import { t } from './i18n'
import {
  askDeleteCategory,
  askDeleteLink,
  askDeleteWorkspace,
  categories,
  cmQuery,
  cmWs,
  curWs,
  links,
  reorderCategories,
  reorderWorkspaces,
  showOver,
  sortedWs,
  togglePin,
  workspaces,
} from './store'
import type { Category, Link, Workspace } from './types'
import { searchLinks } from './util'

function IB(p: { icon: string; label: string; testid: string; onClick: () => void; disabled?: boolean; danger?: boolean; pressed?: boolean; filled?: boolean; size?: number }) {
  return (
    <button
      type="button"
      class={'ib' + (p.danger ? ' danger' : '') + (p.pressed ? ' on' : '')}
      aria-label={p.label}
      title={p.label}
      aria-pressed={p.pressed}
      disabled={p.disabled}
      data-testid={p.testid}
      onClick={p.onClick}
    >
      <Icon n={p.icon} size={p.size || 16} filled={p.filled} />
    </button>
  )
}

function LinkRow({ l, crumb }: { l: Link; crumb?: string }) {
  return (
    <li class="cm-link" data-testid={(crumb ? 'cm-result-' : 'cm-link-') + l.id} data-link-id={l.id}>
      <LinkIcon icon={l.icon} title={l.title} size={30} />
      <div class="cm-link-main">
        {crumb && <span class="cm-crumb">{crumb}</span>}
        <span class="cm-link-t">
          <b>{l.title}</b>
          {l.description && <span class="cm-desc">{l.description}</span>}
        </span>
        <span class="cm-url" title={l.url}>
          {l.url.replace(/^https?:\/\//, '')}
        </span>
      </div>
      <span class="cm-link-tools">
        <IB icon="star" filled={l.pinned} pressed={l.pinned} label={l.pinned ? t('ctx.unpin') : t('ctx.pin')} testid={'cm-pin-' + l.id} onClick={() => togglePin(l)} />
        <IB icon="pencil" label={t('link.edit')} testid={'cm-link-edit-' + l.id} onClick={() => showOver({ kind: 'link', id: l.id })} />
        <IB icon="trash-2" danger label={t('link.delete')} testid={'cm-link-delete-' + l.id} onClick={() => askDeleteLink(l)} />
      </span>
    </li>
  )
}

function Group(p: { ws: Workspace; cat?: Category; items: Link[]; idx: number; total: number; cats: Category[]; open: boolean; toggle: () => void }) {
  const { ws, cat, items } = p
  const key = cat ? cat.id : 'uncat'
  const move = (d: number) => {
    const ids = p.cats.map((c) => c.id)
    const i = ids.indexOf(cat!.id)
    const j = i + d
    if (j < 0 || j >= ids.length) return
    ;[ids[i], ids[j]] = [ids[j], ids[i]]
    reorderCategories(ws.id, ids)
  }
  return (
    <section class="cm-group" data-testid={'cm-cat-' + key}>
      <header class="cm-gh">
        <button type="button" class="cm-toggle" aria-expanded={p.open} data-testid={'cm-toggle-' + key} onClick={p.toggle}>
          <Icon n="chevron-down" size={16} />
          <span class="cm-gname">{cat ? cat.name : t('cat.uncategorized')}</span>
          <small>{items.length}</small>
        </button>
        <span class="cm-gtools">
          {cat && (
            <>
              <IB icon="pencil" label={t('cat.rename')} testid={'cm-cat-rename-' + cat.id} onClick={() => showOver({ kind: 'category', id: cat.id, workspace_id: ws.id })} />
              <IB icon="arrow-up" label={t('ws.up')} testid={'cm-cat-up-' + cat.id} disabled={p.idx === 0} onClick={() => move(-1)} />
              <IB icon="arrow-down" label={t('ws.down')} testid={'cm-cat-down-' + cat.id} disabled={p.idx === p.cats.length - 1} onClick={() => move(1)} />
              <IB icon="trash-2" danger label={t('cat.delete')} testid={'cm-cat-delete-' + cat.id} onClick={() => askDeleteCategory(cat)} />
            </>
          )}
          <button type="button" class="btn sm" aria-label={t('cm.addLink')} title={t('cm.addLink')} data-testid={'cm-add-link-' + key} onClick={() => showOver({ kind: 'link', defaults: { workspace_id: ws.id, category_id: cat ? cat.id : '', pinned: false } })}>
            <Icon n="plus" size={14} />
            <span class="lbl">{t('cm.addLink')}</span>
          </button>
        </span>
      </header>
      {p.open && (items.length ? <ul class="cm-links">{items.map((l) => <LinkRow key={l.id} l={l} />)}</ul> : <p class="cm-none">{t('cm.noLinks')}</p>)}
    </section>
  )
}

export function ContentTab() {
  const list = sortedWs.value
  const sel = list.find((w) => w.id === cmWs.value) || curWs.value || list[0]
  const q = cmQuery.value
  const setQ = (v: string) => (cmQuery.value = v)
  const [closed, setClosed] = useState<Record<string, boolean>>({})
  const allLinks = links.value
  const allCats = categories.value
  const catMap = useMemo(() => new Map(allCats.map((c) => [c.id, c])), [allCats])
  const wsMap = useMemo(() => new Map(workspaces.value.map((w) => [w.id, w])), [workspaces.value])
  const query = q.trim()
  const hits = useMemo(() => (query ? searchLinks(query, allLinks, catMap).slice(0, 60) : []), [query, allLinks, catMap])

  if (!sel) return null
  const wl = allLinks.filter((l) => l.workspace_id === sel.id)
  const cats = allCats.filter((c) => c.workspace_id === sel.id).sort((a, b) => a.sort - b.sort)
  const catIds = new Set(cats.map((c) => c.id))
  const byCat = (id: string) => wl.filter((l) => l.category_id === id).sort((a, b) => a.sort - b.sort)
  const uncat = wl.filter((l) => !catIds.has(l.category_id)).sort((a, b) => a.sort - b.sort)
  const countOf = (id: string) => allLinks.filter((l) => l.workspace_id === id).length
  const i = list.findIndex((w) => w.id === sel.id)
  const moveWs = (d: number) => {
    const ids = list.map((w) => w.id)
    const j = i + d
    if (j < 0 || j >= ids.length) return
    ;[ids[i], ids[j]] = [ids[j], ids[i]]
    reorderWorkspaces(ids)
  }
  const flip = (k: string) => setClosed({ ...closed, [k]: !closed[k] })

  return (
    <div class="cm" data-testid="content-manager">
      <div class="cm-search">
        <Icon n="search" size={16} />
        <input type="text" data-testid="cm-search" aria-label={t('cm.search')} placeholder={t('cm.search')} value={q} onInput={(e) => setQ((e.target as HTMLInputElement).value)} />
        {q && (
          <button type="button" class="ib" aria-label={t('search.clear')} data-testid="cm-search-clear" onClick={() => setQ('')}>
            <Icon n="x" size={14} />
          </button>
        )}
      </div>
      <div class="cm-cols">
        <nav class="cm-side" aria-label={t('ws.label')}>
          <ul class="cm-ws" data-testid="cm-ws-list">
            {list.map((w) => (
              <li key={w.id}>
                <button type="button" class="cm-ws-b" aria-current={w.id === sel.id && !query ? 'true' : undefined} data-testid={'cm-ws-' + w.id} data-ws-name={w.name} onClick={() => ((cmWs.value = w.id), setQ(''))}>
                  <WsIcon icon={w.icon} size={17} />
                  <span class="cm-ws-n">{w.name}</span>
                  <small>{countOf(w.id)}</small>
                </button>
              </li>
            ))}
          </ul>
          <button type="button" class="btn sm cm-ws-add" data-testid="cm-ws-add" onClick={() => showOver({ kind: 'workspace' })}>
            <Icon n="plus" size={14} />
            {t('ws.add')}
          </button>
        </nav>

        <div class="cm-main">
          {query ? (
            <section aria-live="polite">
              <h3 class="cm-title">
                {t('cm.results', { n: hits.length })}
              </h3>
              {hits.length ? (
                <ul class="cm-links" data-testid="cm-results">
                  {hits.map((h) => {
                    const l = h.link
                    const crumb = [wsMap.get(l.workspace_id)?.name, catMap.get(l.category_id)?.name || t('cat.uncategorized')].join(' › ')
                    return <LinkRow key={l.id} l={l} crumb={crumb} />
                  })}
                </ul>
              ) : (
                <p class="cm-none" data-testid="cm-no-results">
                  {t('search.none')}
                </p>
              )}
            </section>
          ) : (
            <>
              <header class="cm-head">
                <span class="cm-head-ico">
                  <WsIcon icon={sel.icon} size={20} />
                </span>
                <h3 class="cm-title" data-testid="cm-ws-title">
                  {sel.name}
                </h3>
                <span class="cm-gtools">
                  <IB icon="pencil" label={t('ws.edit')} testid="cm-ws-edit" onClick={() => showOver({ kind: 'workspace', id: sel.id })} />
                  <IB icon="arrow-up" label={t('ws.up')} testid="cm-ws-up" disabled={i === 0} onClick={() => moveWs(-1)} />
                  <IB icon="arrow-down" label={t('ws.down')} testid="cm-ws-down" disabled={i === list.length - 1} onClick={() => moveWs(1)} />
                  <IB icon="trash-2" danger label={t('ws.delete')} testid="cm-ws-delete" disabled={list.length < 2} onClick={() => askDeleteWorkspace(sel)} />
                </span>
              </header>
              <div class="cm-bar">
                <button type="button" class="btn sm" data-testid="cm-cat-add" onClick={() => showOver({ kind: 'category', workspace_id: sel.id })}>
                  <Icon n="folder-plus" size={15} />
                  {t('cat.new')}
                </button>
                <button type="button" class="btn sm" data-testid="cm-link-add" onClick={() => showOver({ kind: 'link', defaults: { workspace_id: sel.id, category_id: '', pinned: false } })}>
                  <Icon n="plus" size={15} />
                  {t('empty.add')}
                </button>
              </div>
              {cats.map((c, k) => (
                <Group key={c.id} ws={sel} cat={c} items={byCat(c.id)} idx={k} total={cats.length} cats={cats} open={!closed[c.id]} toggle={() => flip(c.id)} />
              ))}
              {(uncat.length > 0 || !cats.length) && <Group ws={sel} items={uncat} idx={0} total={0} cats={cats} open={!closed.uncat} toggle={() => flip('uncat')} />}
            </>
          )}
        </div>
      </div>
    </div>
  )
}
