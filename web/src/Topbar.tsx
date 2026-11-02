import { Icon, WsIcon } from './icons'
import { Dropdown } from './Menu'
import { t } from './i18n'
import { curWs, editMode, isAdmin, logout, modal, openSettings, patchSettings, reorderWorkspaces, role, setWs, settings, showLogin, sortedWs } from './store'
import type { SortMode } from './types'

const MODES: SortMode[] = ['frequency', 'recent', 'custom']

function WorkspaceMenu() {
  const list = sortedWs.value
  const cur = curWs.value
  const edit = editMode.value && isAdmin.value
  const move = (id: string, d: number) => {
    const ids = list.map((w) => w.id)
    const i = ids.indexOf(id)
    const j = i + d
    if (j < 0 || j >= ids.length) return
    ;[ids[i], ids[j]] = [ids[j], ids[i]]
    reorderWorkspaces(ids)
  }
  // keep keyboard focus on the same control after the row jumped to its new position
  const refocus = (testid: string) => requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-testid="${testid}"]`)?.focus())
  return (
    <Dropdown
      id="ws-menu"
      align="left"
      label={t('ws.label')}
      testid="ws-menu"
      class="dd-ws"
      trigger={(p, open) => (
        <button
          {...p}
          class={'menu-btn glass-lite' + (open ? ' on' : '')}
          aria-label={`${t('ws.label')}: ${cur?.name || ''}`}
          title={t('ws.label')}
          data-testid="ws-menu-btn"
          data-ws-name={cur?.name}
        >
          {cur && (
            <span class="mb-ico">
              <WsIcon icon={cur.icon} size={17} />
            </span>
          )}
          <span class="mb-name">{cur?.name}</span>
          <span class="chev">
            <Icon n="chevron-down" size={15} />
          </span>
        </button>
      )}
    >
      {(close) => (
        <>
          {list.map((w, i) => {
            const active = cur?.id === w.id
            return (
              <div
                key={w.id}
                class="mrow"
                role="none"
                onContextMenu={(e) => {
                  if (edit) {
                    e.preventDefault()
                    close()
                    modal.value = { kind: 'workspace', id: w.id }
                  }
                }}
              >
                <button
                  class="mi"
                  role="menuitemradio"
                  aria-checked={active}
                  data-testid={'ws-item-' + w.id}
                  data-ws-name={w.name}
                  onClick={() => (setWs(w.id), close())}
                >
                  <span class="mi-ico">
                    <WsIcon icon={w.icon} size={17} />
                  </span>
                  <span class="mi-name">{w.name}</span>
                  {i < 9 && <kbd class="mi-key">{i + 1}</kbd>}
                  <span class="mi-check">{active && <Icon n="check" size={15} />}</span>
                </button>
                {edit && (
                  <span class="mi-tools">
                    <button class="mini" role="menuitem" aria-label={t('ws.up')} title={t('ws.up')} aria-disabled={i === 0} data-testid={'ws-up-' + w.id} onClick={() => (move(w.id, -1), refocus('ws-up-' + w.id))}>
                      <Icon n="chevron-up" size={13} />
                    </button>
                    <button class="mini" role="menuitem" aria-label={t('ws.down')} title={t('ws.down')} aria-disabled={i === list.length - 1} data-testid={'ws-down-' + w.id} onClick={() => (move(w.id, 1), refocus('ws-down-' + w.id))}>
                      <Icon n="chevron-down" size={13} />
                    </button>
                    <button
                      class="mini"
                      role="menuitem"
                      aria-label={t('ws.edit')}
                      title={t('ws.edit')}
                      data-testid={'ws-edit-' + w.id}
                      onClick={() => (close(), (modal.value = { kind: 'workspace', id: w.id }))}
                    >
                      <Icon n="pencil" size={12} />
                    </button>
                  </span>
                )}
              </div>
            )
          })}
          {isAdmin.value && (
            <>
              <div class="mi-sep" role="separator" />
              <button class="mi" role="menuitem" data-testid="ws-add" onClick={() => (close(), (modal.value = { kind: 'workspace' }))}>
                <span class="mi-ico">
                  <Icon n="plus" size={16} />
                </span>
                <span class="mi-name">{t('ws.add')}</span>
              </button>
              <button class="mi" role="menuitem" data-testid="ws-manage" onClick={() => (close(), openSettings('content'))}>
                <span class="mi-ico">
                  <Icon n="layout-list" size={16} />
                </span>
                <span class="mi-name">{t('ws.manage')}</span>
              </button>
            </>
          )}
        </>
      )}
    </Dropdown>
  )
}

function MainMenu() {
  const admin = isAdmin.value
  const mode = settings.value.sort_mode
  return (
    <Dropdown
      id="main-menu"
      align="right"
      label={t('top.menu')}
      testid="main-menu"
      trigger={(p, open) => (
        <button {...p} class={'icon-btn glass-lite' + (open ? ' on' : '')} aria-label={t('top.menu')} title={t('top.menu')} data-testid="menu-btn">
          <Icon n="sliders-horizontal" />
        </button>
      )}
    >
      {(close) =>
        admin ? (
          <>
            <div class="mi-head" id="sort-head">
              {t('menu.sort')}
            </div>
            <div role="group" aria-labelledby="sort-head" data-testid="sort-control">
              {MODES.map((m) => (
                <button
                  key={m}
                  class="mi"
                  role="menuitemradio"
                  aria-checked={mode === m}
                  data-testid={'sort-' + m}
                  onClick={() => (close(), mode !== m && patchSettings({ sort_mode: m }))}
                >
                  <span class="mi-name">{t('sort.' + m)}</span>
                  <span class="mi-check">{mode === m && <Icon n="check" size={15} />}</span>
                </button>
              ))}
            </div>
            <div class="mi-sep" role="separator" />
            <button class="mi" role="menuitem" data-testid="settings-btn" onClick={() => (close(), openSettings())}>
              <span class="mi-ico">
                <Icon n="settings" size={16} />
              </span>
              <span class="mi-name">{t('top.settings')}</span>
            </button>
            <button class="mi" role="menuitem" data-testid="logout-btn" onClick={() => (close(), logout())}>
              <span class="mi-ico">
                <Icon n="log-out" size={16} />
              </span>
              <span class="mi-name">{t('top.logout')}</span>
            </button>
          </>
        ) : (
          <button class="mi" role="menuitem" data-testid="login-btn" onClick={() => (close(), (showLogin.value = true))}>
            <span class="mi-ico">
              <Icon n="log-in" size={16} />
            </span>
            <span class="mi-name">{t('top.login')}</span>
          </button>
        )
      }
    </Dropdown>
  )
}

export function Topbar() {
  const admin = isAdmin.value
  const edit = editMode.value && admin
  return (
    <header class="topbar">
      <WorkspaceMenu />
      <div class="actions">
        {admin && (
          <button
            class={'icon-btn glass-lite' + (edit ? ' on' : '')}
            aria-label={edit ? t('top.done') : t('top.edit')}
            aria-pressed={edit}
            title={edit ? t('top.done') : t('top.edit')}
            data-testid="edit-toggle"
            onClick={() => (editMode.value = !editMode.value)}
          >
            <Icon n={edit ? 'check' : 'pencil'} />
            {edit && <span class="btn-lbl">{t('top.done')}</span>}
          </button>
        )}
        {admin && (
          <button class="icon-btn glass-lite" aria-label="AI" title="AI" data-testid="ai-btn" onClick={() => openSettings('ai')}>
            <Icon n="bot" />
          </button>
        )}
        {(admin || role.value === 'guest') && <MainMenu />}
      </div>
    </header>
  )
}
