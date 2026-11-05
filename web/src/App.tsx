import { useEffect } from 'preact/hooks'
import { AuthScreen } from './Auth'
import { Board } from './Board'
import { CtxMenu, onPageContextMenu } from './ContextMenu'
import { CategoryModal, ConfirmModal, LinkEditor, WorkspaceModal } from './Editors'
import { Hero, focusSearch, searchFocused } from './Hero'
import { loadPlugins, loadUpdate, loadWidgets, useChunk } from './lazy'
import { Toasts } from './parts'
import { SettingsModal } from './Settings'
import { Topbar } from './Topbar'
import { Wallpaper } from './Wallpaper'
import { WidgetSlot } from './WidgetSlot'
import { t } from './i18n'
import { watchUpdate } from './updState'
import { DEFAULT_WALLPAPER } from './util'
import { curWs, editMode, isAdmin, load, loadError, loaded, modal, role, setWs, setupRequired, settings, showLogin, sortedWs, type ModalState } from './store'

function isTyping(el: Element | null) {
  if (!el) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || (el as HTMLElement).isContentEditable
}

function useGlobalKeys() {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!loaded.value || role.value === 'none') return
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        if (!modal.value) focusSearch()
        return
      }
      if (e.ctrlKey || e.metaKey || e.altKey || modal.value || isTyping(document.activeElement)) return
      if (e.key === '/') {
        e.preventDefault()
        focusSearch()
      } else if (/^[1-9]$/.test(e.key)) {
        const w = sortedWs.value[Number(e.key) - 1]
        if (w) setWs(w.id)
      } else if (e.key === 'Escape' && searchFocused()) {
        ;(document.activeElement as HTMLElement).blur()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [])
}

function useContextMenu() {
  useEffect(() => {
    document.addEventListener('contextmenu', onPageContextMenu)
    return () => document.removeEventListener('contextmenu', onPageContextMenu)
  }, [])
}

function useSwipe() {
  useEffect(() => {
    const el = document
    let sx = 0
    let sy = 0
    let ok = false
    const start = (e: TouchEvent) => {
      const tg = e.target as HTMLElement
      ok = e.touches.length === 1 && !editMode.value && !modal.value && !!tg.closest('.page') && !tg.closest('input,.results,.menu,.ctx')
      sx = e.touches[0].clientX
      sy = e.touches[0].clientY
    }
    const end = (e: TouchEvent) => {
      if (!ok) return
      const dx = e.changedTouches[0].clientX - sx
      const dy = e.changedTouches[0].clientY - sy
      if (Math.abs(dx) < 80 || Math.abs(dx) < Math.abs(dy) * 2) return
      const list = sortedWs.value
      const i = list.findIndex((w) => w.id === curWs.value?.id)
      const n = list[i + (dx < 0 ? 1 : -1)]
      if (n) setWs(n.id)
    }
    el.addEventListener('touchstart', start, { passive: true })
    el.addEventListener('touchend', end, { passive: true })
    return () => {
      el.removeEventListener('touchstart', start)
      el.removeEventListener('touchend', end)
    }
  }, [])
}

function WidgetModalHost({ state }: { state: Extract<ModalState, { kind: 'widget' }> }) {
  const m = useChunk(loadWidgets)
  return m ? <m.WidgetModal state={state} /> : null
}
function PermModalHost({ state }: { state: Extract<ModalState, { kind: 'plugin-perm' }> }) {
  const m = useChunk(loadPlugins)
  return m ? <m.PermModal state={state} /> : null
}

function UpdateModalHost() {
  const m = useChunk(loadUpdate)
  return m ? <m.UpdateModal /> : null
}

function ModalHost() {
  const m = modal.value
  if (!m) return null
  switch (m.kind) {
    case 'link':
      return <LinkEditor key={m.id || 'new'} state={m} />
    case 'workspace':
      return <WorkspaceModal key={m.id || 'new'} state={m} />
    case 'category':
      return <CategoryModal key={m.id || 'new'} state={m} />
    case 'settings':
      return <SettingsModal state={m} />
    case 'confirm':
      return <ConfirmModal state={m} />
    case 'widget':
      return <WidgetModalHost key={m.id || 'new'} state={m} />
    case 'plugin-perm':
      return <PermModalHost state={m} />
    case 'update':
      return <UpdateModalHost />
  }
}

export function App() {
  useGlobalKeys()
  useSwipe()
  useContextMenu()
  useEffect(() => void load(), [])
  const admin = loaded.value && isAdmin.value
  useEffect(() => watchUpdate(admin), [admin])
  const ws = curWs.value
  const title = settings.value.title
  useEffect(() => {
    document.title = title || 'Genkan'
  }, [title])

  let body
  if (!loaded.value) {
    body = loadError.value ? (
      <div class="auth">
        <div class="auth-card glass">
          <h1>{t('err.loadTitle')}</h1>
          <p class="auth-sub">{t('err.load')}</p>
          <button class="btn primary block" data-testid="retry-btn" onClick={() => load()}>
            {t('common.retry')}
          </button>
        </div>
      </div>
    ) : (
      <div class="boot" aria-busy="true" />
    )
  } else if (setupRequired.value) body = <AuthScreen mode="setup" />
  else if (role.value === 'none' || showLogin.value) body = <AuthScreen mode="login" />
  else
    body = (
      <div class="page" data-role={role.value}>
        <Topbar />
        <Hero />
        <WidgetSlot />
        <Board />
        <footer class="foot" aria-hidden="true">
          {isAdmin.value && editMode.value ? t('edit.hint') : ''}
        </footer>
      </div>
    )
  return (
    <>
      <Wallpaper value={ws && loaded.value && role.value !== 'none' && !setupRequired.value && !showLogin.value ? ws.wallpaper : DEFAULT_WALLPAPER} />
      {body}
      <ModalHost />
      <CtxMenu />
      <Toasts />
    </>
  )
}
