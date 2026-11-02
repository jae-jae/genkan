import { useMemo, useRef, useState } from 'preact/hooks'
import { post } from './api'
import { BUILTIN_ICONS } from './lucide'
import { Icon, WsIcon } from './icons'
import { Modal, LinkIcon, Switch } from './parts'
import { t } from './i18n'
import {
  categories,
  closeModal,
  cmWs,
  createCategory,
  createLink,
  createWorkspace,
  curWs,
  askDeleteWorkspace,
  links,
  refreshIcon,
  renameCategory,
  sortedWs,
  toast,
  toastError,
  updateLink,
  updateWorkspace,
  workspaces,
  type ModalState,
} from './store'
import { DEFAULT_WALLPAPER, hostOf, iconMode, normalizeUrl } from './util'
import { WallpaperGallery } from './Gallery'

type Mode = 'auto' | 'emoji' | 'upload' | 'url' | 'letter'
const EMOJIS = ['🏠', '🎬', '🎵', '📺', '📷', '☁️', '🔒', '🛠️', '📊', '🧩', '🌐', '📦', '🎮', '📚', '💾', '🚀']

export function LinkEditor({ state }: { state: Extract<ModalState, { kind: 'link' }> }) {
  const existing = state.id ? links.value.find((l) => l.id === state.id) : undefined
  const def = state.defaults
  const [title, setTitle] = useState(existing?.title || '')
  const [url, setUrl] = useState(existing?.url || '')
  const [desc, setDesc] = useState(existing?.description || '')
  const [wsId, setWsId] = useState(existing?.workspace_id || def?.workspace_id || curWs.value?.id || '')
  const [catId, setCatId] = useState(existing ? existing.category_id : def?.category_id || '')
  const [pinned, setPinned] = useState(existing?.pinned ?? def?.pinned ?? false)
  const [health, setHealth] = useState(existing?.health_check ?? false)
  const [mode, setMode] = useState<Mode>(existing ? iconMode(existing.icon) : 'auto')
  const [emoji, setEmoji] = useState(existing?.icon.startsWith('emoji:') ? existing.icon.slice(6) : '')
  const [imgUrl, setImgUrl] = useState(existing?.icon.startsWith('url:') ? existing.icon.slice(4) : '')
  const [upload, setUpload] = useState(existing?.icon.startsWith('upload:') ? existing.icon : '')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const autoTitle = useRef(!existing)
  const cats = useMemo(() => categories.value.filter((c) => c.workspace_id === wsId).sort((a, b) => a.sort - b.sort), [wsId, categories.value])

  const onUrl = (v: string) => {
    setUrl(v)
    if (autoTitle.current) {
      const h = hostOf(v)
      setTitle(h)
    }
  }
  const onWs = (id: string) => {
    setWsId(id)
    setCatId('')
  }
  const icon =
    mode === 'emoji' ? (emoji.trim() ? 'emoji:' + emoji.trim() : '') : mode === 'url' ? (imgUrl.trim() ? 'url:' + imgUrl.trim() : '') : mode === 'upload' ? upload : mode === 'auto' ? (existing?.icon.startsWith('favicon:') ? existing.icon : '') : ''

  const doUpload = async (f: File | undefined) => {
    if (!f) return
    const fd = new FormData()
    fd.append('file', f)
    try {
      const r = await post<{ value: string }>('/api/uploads/icon', fd)
      setUpload(r.value)
    } catch (e) {
      toastError(e)
    }
  }

  const save = async (ev: Event) => {
    ev.preventDefault()
    const u = normalizeUrl(url)
    if (!u) return setErr(t('link.errUrl'))
    const ttl = title.trim() || hostOf(u) || u
    setBusy(true)
    const body = { workspace_id: wsId, category_id: catId, title: ttl, url: u, description: desc.trim(), icon, pinned, health_check: health }
    if (existing) {
      const needFetch = mode === 'auto' && !existing.icon.startsWith('favicon:')
      const l = await updateLink(existing.id, body)
      setBusy(false)
      if (l) {
        closeModal()
        if (needFetch) refreshIcon(existing.id)
      }
    } else {
      const l = await createLink({ ...body, fetch_icon: mode === 'auto' })
      setBusy(false)
      if (l) {
        closeModal()
        toast(t('toast.linkAdded'), 'success')
      }
    }
  }

  const modes: [Mode, string][] = [
    ['auto', t('icon.auto')],
    ['emoji', t('icon.emoji')],
    ['upload', t('icon.upload')],
    ['url', t('icon.url')],
    ['letter', t('icon.letter')],
  ]
  return (
    <Modal
      title={existing ? t('link.editTitle') : t('link.newTitle')}
      onClose={closeModal}
      testid="link-modal"
      footer={
        <>
          <button type="button" class="btn" data-testid="link-cancel" onClick={closeModal}>
            {t('common.cancel')}
          </button>
          <button type="submit" form="link-form" class="btn primary" disabled={busy} data-testid="link-save">
            {t('common.save')}
          </button>
        </>
      }
    >
      <form id="link-form" class="form" onSubmit={save}>
        <div class="field">
          <label for="f-url">{t('link.url')}</label>
          <input id="f-url" data-testid="link-url" type="text" inputMode="url" autocapitalize="off" spellcheck={false} placeholder="http://nas.lan:8096" value={url} onInput={(e) => (setErr(''), onUrl((e.target as HTMLInputElement).value))} />
        </div>
        <div class="field">
          <label for="f-title">{t('link.title')}</label>
          <input id="f-title" data-testid="link-title" type="text" placeholder="Jellyfin" value={title} onInput={(e) => ((autoTitle.current = false), setTitle((e.target as HTMLInputElement).value))} />
        </div>
        <div class="field">
          <label for="f-desc">{t('link.desc')}</label>
          <input id="f-desc" data-testid="link-desc" type="text" value={desc} onInput={(e) => setDesc((e.target as HTMLInputElement).value)} />
        </div>
        <div class="field-row">
          <div class="field">
            <label for="f-ws">{t('link.workspace')}</label>
            <select id="f-ws" data-testid="link-workspace" value={wsId} onChange={(e) => onWs((e.target as HTMLSelectElement).value)}>
              {sortedWs.value.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.name}
                </option>
              ))}
            </select>
          </div>
          <div class="field">
            <label for="f-cat">{t('link.category')}</label>
            <select id="f-cat" data-testid="link-category" value={catId} onChange={(e) => setCatId((e.target as HTMLSelectElement).value)}>
              <option value="">{t('cat.uncategorized')}</option>
              {cats.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>
        </div>
        <div class="field">
          <label>{t('link.icon')}</label>
          <div class="icon-edit">
            <div class="icon-prev">
              <LinkIcon icon={icon} title={title || hostOf(url) || '?'} size={40} />
            </div>
            <div class="icon-ctl">
              <div class="seg seg-sm glass-lite" role="group" aria-label={t('link.icon')} data-testid="icon-mode">
                {modes.map(([m, label]) => (
                  <button type="button" key={m} class="seg-b" aria-pressed={mode === m} data-testid={'icon-mode-' + m} onClick={() => setMode(m)}>
                    {label}
                  </button>
                ))}
              </div>
              {mode === 'auto' && (
                <p class="hint">
                  {existing?.icon.startsWith('favicon:') ? t('icon.autoHave') : t('icon.autoHint')}
                  {existing && (
                    <button type="button" class="linkbtn" data-testid="link-refresh-icon" onClick={() => refreshIcon(existing.id).then((l) => l && toast(t('toast.iconUpdated'), 'success'))}>
                      <Icon n="refresh-cw" size={13} /> {t('icon.refetch')}
                    </button>
                  )}
                </p>
              )}
              {mode === 'emoji' && (
                <div>
                  <input data-testid="link-icon-emoji" type="text" maxLength={8} placeholder="🚀" aria-label={t('icon.emoji')} value={emoji} onInput={(e) => setEmoji((e.target as HTMLInputElement).value)} />
                  <div class="emoji-grid">
                    {EMOJIS.map((x) => (
                      <button type="button" key={x} class="emoji-b" onClick={() => setEmoji(x)} aria-label={x}>
                        {x}
                      </button>
                    ))}
                  </div>
                </div>
              )}
              {mode === 'upload' && (
                <label class="btn upload-btn">
                  <Icon n="upload" size={15} />
                  {upload ? t('icon.replace') : t('icon.choose')}
                  <input data-testid="link-icon-file" type="file" accept="image/png,image/svg+xml,image/x-icon,image/webp,image/jpeg" hidden onChange={(e) => doUpload((e.target as HTMLInputElement).files?.[0])} />
                </label>
              )}
              {mode === 'url' && <input data-testid="link-icon-url" type="url" placeholder="https://…/icon.png" aria-label={t('icon.url')} value={imgUrl} onInput={(e) => setImgUrl((e.target as HTMLInputElement).value)} />}
              {mode === 'letter' && <p class="hint">{t('icon.letterHint')}</p>}
            </div>
          </div>
        </div>
        <div class="checks">
          <div class="check">
            <span>{t('link.pinned')}</span>
            <Switch label={t('link.pinned')} checked={pinned} onChange={setPinned} testid="link-pinned" />
          </div>
          <div class="check">
            <span>
              {t('link.health')}
              <small>{t('link.healthHint')}</small>
            </span>
            <Switch label={t('link.health')} checked={health} onChange={setHealth} testid="link-health" />
          </div>
        </div>
        {err && (
          <p class="form-error" role="alert">
            {err}
          </p>
        )}
      </form>
    </Modal>
  )
}

export function CategoryModal({ state }: { state: Extract<ModalState, { kind: 'category' }> }) {
  const existing = state.id ? categories.value.find((c) => c.id === state.id) : undefined
  const [name, setName] = useState(existing?.name || '')
  const save = (e: Event) => {
    e.preventDefault()
    const n = name.trim()
    if (!n) return
    closeModal()
    if (existing) renameCategory(existing.id, n)
    else createCategory(state.workspace_id, n)
  }
  return (
    <Modal
      title={existing ? t('cat.renameTitle') : t('cat.newTitle')}
      onClose={closeModal}
      testid="category-modal"
      footer={
        <>
          <button type="button" class="btn" data-testid="category-cancel" onClick={closeModal}>
            {t('common.cancel')}
          </button>
          <button type="submit" form="cat-form" class="btn primary" data-testid="category-save" disabled={!name.trim()}>
            {t('common.save')}
          </button>
        </>
      }
    >
      <form id="cat-form" class="form" onSubmit={save}>
        <div class="field">
          <label for="c-name">{t('cat.name')}</label>
          <input id="c-name" data-testid="category-name" type="text" maxLength={60} value={name} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
        </div>
      </form>
    </Modal>
  )
}

export function WorkspaceModal({ state }: { state: Extract<ModalState, { kind: 'workspace' }> }) {
  const existing = state.id ? workspaces.value.find((w) => w.id === state.id) : undefined
  const [name, setName] = useState(existing?.name || '')
  const [icon, setIcon] = useState(existing && existing.icon.startsWith('lucide:') ? existing.icon : 'lucide:layout-grid')
  const [wp, setWp] = useState(existing?.wallpaper || DEFAULT_WALLPAPER)
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState(false)
  const shown = BUILTIN_ICONS.filter((n) => !q.trim() || n.includes(q.trim().toLowerCase().replace(/\s+/g, '-')))
  const save = async (e: Event) => {
    e.preventDefault()
    const n = name.trim()
    if (!n) return
    setBusy(true)
    const r = existing ? await updateWorkspace(existing.id, { name: n, icon, wallpaper: wp }) : await createWorkspace({ name: n, icon, wallpaper: wp })
    setBusy(false)
    if (r) {
      if (!existing) cmWs.value = r.id
      closeModal()
    }
  }
  return (
    <Modal
      title={existing ? t('ws.editTitle') : t('ws.newTitle')}
      onClose={closeModal}
      testid="workspace-modal"
      cls="modal-ws"
      footer={
        <>
          {existing && (
            <button type="button" class="btn danger left" data-testid="ws-delete" disabled={workspaces.value.length < 2} title={workspaces.value.length < 2 ? t('ws.lastOne') : ''} onClick={() => askDeleteWorkspace(existing)}>
              {t('common.delete')}
            </button>
          )}
          <button type="button" class="btn" data-testid="ws-cancel" onClick={closeModal}>
            {t('common.cancel')}
          </button>
          <button type="submit" form="ws-form" class="btn primary" data-testid="ws-save" disabled={busy || !name.trim()}>
            {t('common.save')}
          </button>
        </>
      }
    >
      <form id="ws-form" class="form" onSubmit={save}>
        <div class="ws-row">
          <div class="ws-prev" aria-hidden="true" data-testid="ws-icon-preview">
            <WsIcon icon={icon} size={24} />
          </div>
          <div class="field">
            <label for="w-name">{t('ws.name')}</label>
            <input id="w-name" data-testid="ws-name" type="text" maxLength={40} value={name} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
          </div>
        </div>
        <div class="field">
          <div class="field-line">
            <label id="w-icon-l">{t('ws.icon')}</label>
            <span class="search-mini">
              <Icon n="search" size={14} />
              <input type="text" data-testid="ws-icon-search" aria-label={t('ws.iconSearch')} placeholder={t('ws.iconSearch')} value={q} onInput={(e) => setQ((e.target as HTMLInputElement).value)} onKeyDown={(e) => e.key === 'Enter' && e.preventDefault()} />
            </span>
          </div>
          <div class="icon-grid" role="radiogroup" aria-labelledby="w-icon-l" data-testid="ws-icon" data-value={icon}>
            {shown.map((n) => (
              <button type="button" key={n} class="icon-cell" role="radio" aria-checked={icon === 'lucide:' + n} aria-label={n} title={n} data-testid={'ws-icon-' + n} onClick={() => setIcon('lucide:' + n)}>
                <Icon n={n} size={19} />
              </button>
            ))}
            {!shown.length && <span class="hint">{t('ws.iconNone')}</span>}
          </div>
        </div>
        <div class="field">
          <label>{t('set.wallpaper')}</label>
          <WallpaperGallery value={wp} onPick={setWp} />
        </div>
      </form>
    </Modal>
  )
}

export function ConfirmModal({ state }: { state: Extract<ModalState, { kind: 'confirm' }> }) {
  const [chk, setChk] = useState(false)
  return (
    <Modal
      title={state.title}
      onClose={closeModal}
      testid="confirm-modal"
      focusFirst={false}
      footer={
        <>
          <button class="btn" data-testid="confirm-cancel" onClick={closeModal}>
            {t('common.cancel')}
          </button>
          <button
            class="btn danger-solid"
            data-testid="confirm-ok"
            autofocus
            ref={(el) => {
              if (el) setTimeout(() => el.focus(), 30)
            }}
            onClick={() => {
              closeModal()
              state.onConfirm(chk)
            }}
          >
            {state.confirmLabel}
          </button>
        </>
      }
    >
      <p class="confirm-msg">{state.message}</p>
      {state.checkbox && (
        <label class="chk">
          <input type="checkbox" data-testid="confirm-checkbox" checked={chk} onChange={(e) => setChk((e.target as HTMLInputElement).checked)} />
          <span>{state.checkbox}</span>
        </label>
      )}
    </Modal>
  )
}
