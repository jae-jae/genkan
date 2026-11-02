import { useRef } from 'preact/hooks'
import { openCtx, linkMenu } from './ContextMenu'
import { beginDrag } from './dnd'
import { Icon } from './icons'
import { LinkIcon } from './parts'
import { t } from './i18n'
import {
  askDeleteCategory,
  askDeleteLink,
  categories,
  curWs,
  dropLink,
  editMode,
  health,
  isAdmin,
  links,
  modal,
  openSettings,
  reorderCategories,
  settings,
  trackClick,
} from './store'
import type { Category, Link } from './types'
import { safeHref, sortLinks } from './util'

function Dot({ l, pre }: { l: Link; pre: string }) {
  const h = health.value[l.id]
  const st = h?.status || 'unknown'
  const label = st === 'up' ? t('health.up') : st === 'down' ? t('health.down') : t('health.unknown')
  return (
    <span
      class="dot"
      data-status={st}
      data-testid={pre + 'health-' + l.id}
      title={h?.latency_ms && st === 'up' ? `${label} · ${h.latency_ms} ms` : label}
      role="img"
      aria-label={label}
    />
  )
}

const LONG_PRESS = 500

/** `pre` prefixes the testids of the pinned-row copy of a capsule ('pin-') so ids stay unique. */
function Cap({ l, edit, pre }: { l: Link; edit: boolean; pre: string }) {
  const tip = [l.description, l.url].filter(Boolean).join('\n')
  const lp = useRef({ timer: 0, x: 0, y: 0, fired: false })
  const inner = (
    <>
      <LinkIcon icon={l.icon} title={l.title} size={l.description ? 30 : 24} />
      <span class="cap-text">
        <span class="cap-title">{l.title}</span>
        {l.description && <span class="cap-desc">{l.description}</span>}
      </span>
      {l.health_check && <Dot l={l} pre={pre} />}
    </>
  )
  const menu = (e: MouseEvent) => {
    e.preventDefault()
    if (edit && (e as PointerEvent).pointerType === 'touch') return // long-press is drag in edit mode
    openCtx(e.clientX, e.clientY, linkMenu(l), 'ctx-menu-link', l.title)
  }
  if (!edit) {
    const nt = settings.value.open_in_new_tab
    const cancel = () => clearTimeout(lp.current.timer)
    return (
      <a
        class="cap glass-lite"
        href={safeHref(l.url)}
        target={nt ? '_blank' : '_self'}
        rel="noopener"
        title={tip}
        data-testid={pre + 'link-' + l.id}
        data-link-id={l.id}
        data-dnd-kind="link"
        data-dnd-item={l.id}
        onClick={(e) => {
          if (lp.current.fired) return e.preventDefault()
          trackClick(l)
        }}
        onAuxClick={(e) => e.button === 1 && trackClick(l)}
        onContextMenu={menu}
        onPointerDown={(e) => {
          if (e.pointerType === 'mouse') return
          const p = lp.current
          p.x = e.clientX
          p.y = e.clientY
          p.fired = false
          cancel()
          p.timer = window.setTimeout(() => {
            p.fired = true
            openCtx(p.x, p.y, linkMenu(l), 'ctx-menu-link', l.title)
            setTimeout(() => (p.fired = false), 700)
          }, LONG_PRESS)
        }}
        onPointerMove={(e) => Math.hypot(e.clientX - lp.current.x, e.clientY - lp.current.y) > 8 && cancel()}
        onPointerUp={cancel}
        onPointerCancel={cancel}
        onPointerLeave={cancel}
        draggable={false}
      >
        {inner}
      </a>
    )
  }
  return (
    <div
      class="cap cap-edit glass-lite"
      title={tip}
      data-testid={pre + 'link-' + l.id}
      data-link-id={l.id}
      data-dnd-kind="link"
      data-dnd-item={l.id}
      onClick={() => (modal.value = { kind: 'link', id: l.id })}
      onContextMenu={menu}
      onPointerDown={(e) =>
        beginDrag(e, {
          kind: 'link',
          id: l.id,
          el: e.currentTarget as HTMLElement,
          onDrop: (r) => dropLink(l.id, r.containerKey, r.index, r.containers, pre === 'pin-'),
        })
      }
    >
      {inner}
      <span class="cap-actions">
        <button class="mini" aria-label={t('link.edit')} title={t('link.edit')} data-testid={pre + 'link-edit-' + l.id} onClick={(e) => (e.stopPropagation(), (modal.value = { kind: 'link', id: l.id }))}>
          <Icon n="pencil" size={12} />
        </button>
        <button
          class="mini danger"
          aria-label={t('link.delete')}
          title={t('link.delete')}
          data-testid={pre + 'link-delete-' + l.id}
          onClick={(e) => {
            e.stopPropagation()
            askDeleteLink(l)
          }}
        >
          <Icon n="x" size={12} />
        </button>
      </span>
    </div>
  )
}

interface SecProps {
  ckey: string // pinned | uncat | category id
  title: string
  items: Link[]
  edit: boolean
  cat?: Category
  showTitle: boolean
  testid: string
  addDefaults: { workspace_id: string; category_id: string; pinned: boolean }
  addTestid: string
}

function Section(p: SecProps) {
  const { cat, edit } = p
  const pinnedRow = p.ckey === 'pinned'
  const pre = pinnedRow ? 'pin-' : ''
  return (
    <section
      class={'sec' + (pinnedRow ? ' sec-pinned' : '')}
      data-testid={p.testid}
      data-dnd-container={p.ckey}
      {...(cat ? { 'data-dnd-kind': 'category', 'data-dnd-item': cat.id } : {})}
    >
      {!pinnedRow && (p.showTitle || edit) && (
        <header class="sec-head">
          {edit && cat && (
            <span
              class="grip"
              data-handle
              role="button"
              tabIndex={0}
              aria-label={t('cat.drag')}
              title={t('cat.drag')}
              data-testid={'category-handle-' + cat.id}
              onPointerDown={(e) => {
                const sec = (e.currentTarget as HTMLElement).closest('section') as HTMLElement
                beginDrag(e, {
                  kind: 'category',
                  id: cat.id,
                  el: sec,
                  onDrop: (r) => {
                    const all = categories.value.filter((c) => c.workspace_id === cat.workspace_id).sort((a, b) => a.sort - b.sort).map((c) => c.id)
                    const ids = all.filter((x) => x !== cat.id)
                    ids.splice(r.index, 0, cat.id)
                    if (ids.join() !== all.join()) reorderCategories(cat.workspace_id, ids)
                  },
                })
              }}
            >
              <Icon n="grip-vertical" size={16} />
            </span>
          )}
          <h2 class="sec-title" data-testid={p.testid + '-title'}>
            {p.title}
          </h2>
          {edit && cat && (
            <span class="sec-tools">
              <button class="mini" aria-label={t('cat.rename')} title={t('cat.rename')} data-testid={'category-rename-' + cat.id} onClick={() => (modal.value = { kind: 'category', id: cat.id, workspace_id: cat.workspace_id })}>
                <Icon n="pencil" size={12} />
              </button>
              <button class="mini danger" aria-label={t('cat.delete')} title={t('cat.delete')} data-testid={'category-delete-' + cat.id} onClick={() => askDeleteCategory(cat)}>
                <Icon n="trash-2" size={12} />
              </button>
            </span>
          )}
        </header>
      )}
      <div class={'flow' + (p.items.some((l) => l.description) ? ' has-desc' : '')} data-dnd-flow>
        {p.items.map((l) => (
          <Cap key={l.id} l={l} edit={edit} pre={pre} />
        ))}
        {edit && pinnedRow && !p.items.length && <span class="flow-hint">{t('pinned.hint')}</span>}
        {edit && (
          <button class="cap cap-add glass-lite" aria-label={t('link.add')} title={t('link.add')} data-testid={p.addTestid} onClick={() => (modal.value = { kind: 'link', defaults: p.addDefaults })}>
            <Icon n="plus" size={16} />
          </button>
        )}
      </div>
    </section>
  )
}

export function Board() {
  const ws = curWs.value
  const admin = isAdmin.value
  const edit = editMode.value && admin
  if (!ws) return null
  const mode = settings.value.sort_mode
  const cats = categories.value.filter((c) => c.workspace_id === ws.id).sort((a, b) => a.sort - b.sort)
  const catIds = new Set(cats.map((c) => c.id))
  const wl = links.value.filter((l) => l.workspace_id === ws.id)
  // pinned links show in the top row AND in their own category
  const pinned = sortLinks(wl.filter((l) => l.pinned), mode, true)
  const uncat = sortLinks(wl.filter((l) => !l.category_id || !catIds.has(l.category_id)), mode)
  // An empty workspace shows the onboarding card; in edit mode the (empty) sections are shown instead so
  // categories and the first links can be added.
  const empty = wl.length === 0 && !edit

  if (empty)
    return (
      <main class="board" key={ws.id}>
        <div class="empty glass" data-testid="empty-state">
          <div class="empty-ico">
            <Icon n="compass" size={34} />
          </div>
          <h2>{t(admin ? 'empty.title' : 'empty.titleGuest')}</h2>
          <p>{t(admin ? 'empty.desc' : 'empty.descGuest')}</p>
          {admin && (
            <div class="empty-actions">
              <button class="btn primary" data-testid="empty-add-link" onClick={() => (modal.value = { kind: 'link', defaults: { workspace_id: ws.id, category_id: '', pinned: false } })}>
                <Icon n="plus" size={16} />
                {t('empty.add')}
              </button>
              <button class="btn" data-testid="empty-mcp" onClick={() => openSettings('ai')}>
                {t('empty.mcp')}
              </button>
            </div>
          )}
        </div>
      </main>
    )

  const def = (category_id: string, pinned = false) => ({ workspace_id: ws.id, category_id, pinned })
  return (
    <main class="board" key={ws.id} data-testid="board" data-editing={edit ? '1' : undefined}>
      {edit && (
        <div class="toolbar">
          <button class="btn glass-lite" data-testid="new-category-btn" onClick={() => (modal.value = { kind: 'category', workspace_id: ws.id })}>
            <Icon n="plus" size={15} />
            {t('cat.new')}
          </button>
        </div>
      )}
      {(pinned.length > 0 || edit) && (
        <Section ckey="pinned" title="" items={pinned} edit={edit} showTitle={false} testid="section-pinned" addDefaults={def('', true)} addTestid="pinned-add-link" />
      )}
      {cats.map((c) => {
        const items = sortLinks(wl.filter((l) => l.category_id === c.id), mode)
        if (!items.length && !edit) return null
        return <Section key={c.id} ckey={c.id} cat={c} title={c.name} items={items} edit={edit} showTitle testid={'category-' + c.id} addDefaults={def(c.id)} addTestid={'category-add-link-' + c.id} />
      })}
      {(uncat.length > 0 || edit) && (
        <Section
          ckey="uncat"
          title={t('cat.uncategorized')}
          items={uncat}
          edit={edit}
          showTitle={cats.length > 0}
          testid="category-uncategorized"
          addDefaults={def('')}
          addTestid="uncat-add-link"
        />
      )}
    </main>
  )
}
