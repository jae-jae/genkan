// "Data" dialog of an installed plugin: what it keeps in its key/value storage (usage bar, keys, values; delete / clear).
// Lazy chunk, only downloaded when someone opens it.
import { useEffect, useState } from 'preact/hooks'
import { del, get } from './api'
import { Icon } from './icons'
import { loc, t } from './i18n'
import { Modal } from './parts'
import { closeModal, toast, toastError } from './store'
import type { PluginInfo } from './types'

interface Entry {
  key: string
  size: number
  value: unknown
  instance?: string
}
interface Storage {
  size: number
  limit: number
  entries: Entry[] | null
}

const MAX = 20000
const bytes = (n: number) => (n < 1024 ? n + ' B' : (n / 1024).toFixed(n < 10240 ? 1 : 0) + ' KB')
const pretty = (v: unknown) => {
  let s: string
  try {
    s = JSON.stringify(v, null, 2) ?? ''
  } catch {
    s = String(v)
  }
  return s.length > MAX ? s.slice(0, MAX) + '\n… ' + t('plugin.dataTrunc') : s
}
const rowId = (e: Entry) => (e.instance || '') + '\u0000' + e.key

export default function PluginData({ plugin }: { plugin: PluginInfo }) {
  const [st, setSt] = useState<Storage | null>(null)
  const [openRows, setOpenRows] = useState<Record<string, boolean>>({})
  const [ask, setAsk] = useState(false)
  const [busy, setBusy] = useState(false)
  const base = '/api/plugins/' + plugin.id + '/storage'
  const fetchData = async () => {
    try {
      setSt(await get<Storage>(base))
    } catch (e) {
      toastError(e)
      closeModal()
    }
  }
  useEffect(() => void fetchData(), [plugin.id])

  const remove = async (e: Entry) => {
    setBusy(true)
    try {
      await del(base + '?key=' + encodeURIComponent(e.key) + (e.instance ? '&instance=' + encodeURIComponent(e.instance) : ''))
      await fetchData()
    } catch (err) {
      toastError(err)
    }
    setBusy(false)
  }
  const clearAll = async () => {
    setBusy(true)
    setAsk(false)
    try {
      await del(base)
      toast(t('plugin.dataCleared'), 'success')
      await fetchData()
    } catch (err) {
      toastError(err)
    }
    setBusy(false)
  }

  const entries = st?.entries || []
  const pct = st ? Math.min(100, Math.max(entries.length ? 1.5 : 0, (st.size / (st.limit || 1)) * 100)) : 0
  return (
    <Modal
      title={t('plugin.dataTitle', { name: loc(plugin.name) })}
      onClose={closeModal}
      testid="plugin-data-modal"
      focusFirst={false}
      wide
      footer={
        <>
          {ask ? (
            <span class="pd-ask" data-testid="plugin-data-confirm">
              <span>{t('plugin.dataClearAsk')}</span>
              <button class="btn" data-testid="plugin-data-clear-cancel" onClick={() => setAsk(false)}>
                {t('common.cancel')}
              </button>
              <button class="btn danger-solid" data-testid="plugin-data-clear-yes" disabled={busy} autofocus onClick={clearAll}>
                {t('plugin.dataClearYes')}
              </button>
            </span>
          ) : (
            <button class="btn danger" data-testid="plugin-data-clear" disabled={busy || !entries.length} onClick={() => setAsk(true)}>
              <Icon n="trash-2" size={15} />
              {t('plugin.dataClear')}
            </button>
          )}
          <button class="btn" data-testid="plugin-data-close" onClick={closeModal}>
            {t('common.close')}
          </button>
        </>
      }
    >
      <p class="hint pd-hint">{t('plugin.dataHint')}</p>
      {st && (
        <div class="pd-usage" data-testid="plugin-data-usage" data-size={st.size} data-limit={st.limit}>
          <div class="pd-bar" role="progressbar" aria-valuemin={0} aria-valuemax={st.limit} aria-valuenow={st.size}>
            <i style={{ width: pct + '%' }} />
          </div>
          <small>{t('plugin.dataUsage', { used: bytes(st.size), limit: bytes(st.limit) })}</small>
        </div>
      )}
      {!st ? (
        <div class="pd-empty" aria-busy="true">
          …
        </div>
      ) : !entries.length ? (
        <div class="pd-empty" data-testid="plugin-data-empty">
          {t('plugin.dataEmpty')}
        </div>
      ) : (
        <ul class="pd-list" data-testid="plugin-data-list">
          {entries.map((e) => {
            const id = rowId(e)
            const o = !!openRows[id]
            return (
              <li class="pd-row" key={id} data-testid="plugin-data-row" data-key={e.key} data-instance={e.instance || ''}>
                <div class="pd-head">
                  <button class="pd-main" aria-expanded={o} data-testid="plugin-data-toggle" onClick={() => setOpenRows({ ...openRows, [id]: !o })}>
                    <Icon n={o ? 'chevron-down' : 'chevron-right'} size={15} />
                    <code class="pd-key">{e.key}</code>
                    {e.instance && (
                      <span class="wg-badge" title={e.instance} data-testid="plugin-data-instance">
                        {t('plugin.dataInst', { id: e.instance.slice(0, 6) })}
                      </span>
                    )}
                  </button>
                  <small class="pd-size" data-testid="plugin-data-size">
                    {bytes(e.size)}
                  </small>
                  <button class="ib danger xs" disabled={busy} aria-label={t('plugin.dataDel', { key: e.key })} title={t('plugin.dataDel', { key: e.key })} data-testid="plugin-data-delete" onClick={() => remove(e)}>
                    <Icon n="trash-2" size={14} />
                  </button>
                </div>
                {o && (
                  <pre class="pl-logs pd-val" data-testid="plugin-data-value" tabIndex={0}>
                    {pretty(e.value)}
                  </pre>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </Modal>
  )
}
