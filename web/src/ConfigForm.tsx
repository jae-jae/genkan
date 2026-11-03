import { useState } from 'preact/hooks'
import { Switch } from './parts'
import { loc, t } from './i18n'
import type { ConfigField } from './types'

type Vals = Record<string, string | boolean>

/** Form state generated from a plugin's ConfigField[]. Inputs hold strings; `build()` converts to the typed config object. */
export function useConfigForm(schema: ConfigField[], saved: Record<string, any> | null | undefined, secretsSet: string[]) {
  const init = (): Vals => {
    const v: Vals = {}
    for (const f of schema) {
      const cur = saved?.[f.key]
      if (f.type === 'boolean') v[f.key] = cur === undefined || cur === null ? !!f.default : !!cur
      else if (f.type === 'secret') v[f.key] = ''
      else v[f.key] = cur === undefined || cur === null ? (f.default === undefined || f.default === null ? '' : String(f.default)) : String(cur)
    }
    return v
  }
  const [vals, setVals] = useState<Vals>(init)
  const [clear, setClear] = useState<Record<string, boolean>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const set = (k: string, v: string | boolean) => {
    setVals((x) => ({ ...x, [k]: v }))
    setErrors((e) => (e[k] ? { ...e, [k]: '' } : e))
  }
  const validate = (): boolean => {
    const e: Record<string, string> = {}
    for (const f of schema) {
      const v = vals[f.key]
      if (f.type === 'boolean') continue
      const s = String(v ?? '').trim()
      if (f.type === 'secret') {
        if (f.required && !s && (clear[f.key] || !secretsSet.includes(f.key))) e[f.key] = t('cfg.required')
        continue
      }
      if (!s) {
        if (f.required) e[f.key] = t('cfg.required')
        continue
      }
      if (f.type === 'number') {
        const n = Number(s)
        if (!Number.isFinite(n)) e[f.key] = t('cfg.nan')
        else if ((f.min !== undefined && n < f.min) || (f.max !== undefined && n > f.max)) e[f.key] = t('cfg.range', { min: f.min ?? '-∞', max: f.max ?? '∞' })
      } else if (f.type === 'url' && !/^https?:\/\/[^\s/]+/i.test(s)) e[f.key] = t('cfg.badUrl')
    }
    setErrors(e)
    return !Object.keys(e).length
  }
  /** Secrets: "" keeps the stored value, null clears it (docs/PLUGINS.md §1). */
  const build = (): Record<string, any> => {
    const out: Record<string, any> = {}
    for (const f of schema) {
      const v = vals[f.key]
      if (f.type === 'boolean') out[f.key] = !!v
      else if (f.type === 'secret') out[f.key] = clear[f.key] ? null : String(v ?? '')
      else if (f.type === 'number') {
        const s = String(v ?? '').trim()
        if (s) out[f.key] = Number(s)
      } else out[f.key] = typeof v === 'string' && f.type !== 'text' ? v.trim() : v
    }
    return out
  }
  return { vals, set, clear, toggleClear: (k: string) => setClear((c) => ({ ...c, [k]: !c[k] })), errors, validate, build }
}

export type ConfigFormState = ReturnType<typeof useConfigForm>

/** Renders the generated fields. `pre` prefixes the testids: `<pre>-<key>` for controls, `<pre>-secret-clear-<key>`, `<pre>-error-<key>`. */
export function ConfigFields({ schema, form, secretsSet, pre }: { schema: ConfigField[]; form: ConfigFormState; secretsSet: string[]; pre: string }) {
  return (
    <>
      {schema.map((f) => (
        <Field key={f.key} f={f} form={form} isSet={secretsSet.includes(f.key)} pre={pre} />
      ))}
    </>
  )
}

function Field({ f, form, isSet, pre }: { f: ConfigField; form: ConfigFormState; isSet: boolean; pre: string }) {
  const [id] = useState(() => 'cf-' + Math.random().toString(36).slice(2, 8))
  const v = form.vals[f.key]
  const err = form.errors[f.key]
  const tid = pre + '-' + f.key
  const inp = (e: Event) => form.set(f.key, (e.target as HTMLInputElement).value)
  let control
  switch (f.type) {
    case 'boolean':
      control = <Switch id={id} label={loc(f.label)} checked={!!v} onChange={(x) => form.set(f.key, x)} testid={tid} />
      break
    case 'text':
      control = <textarea id={id} data-testid={tid} rows={4} value={String(v ?? '')} onInput={inp} />
      break
    case 'number':
      control = <input id={id} type="number" data-testid={tid} min={f.min} max={f.max} step="any" value={String(v ?? '')} onInput={inp} />
      break
    case 'select':
      control = (
        <select id={id} data-testid={tid} value={String(v ?? '')} onChange={inp}>
          {!f.required && !(f.options || []).some((o) => o.value === '') && <option value="" />}
          {(f.options || []).map((o) => (
            <option key={o.value} value={o.value}>
              {loc(o.label)}
            </option>
          ))}
        </select>
      )
      break
    case 'secret': {
      const cleared = !!form.clear[f.key]
      control = (
        <div class="cfg-secret">
          <input
            id={id}
            type="password"
            autocomplete="new-password"
            data-testid={tid}
            data-set={isSet && !cleared ? '1' : '0'}
            disabled={cleared}
            placeholder={cleared ? t('cfg.willClear') : isSet ? t('cfg.secretKeep') : ''}
            value={String(v ?? '')}
            onInput={inp}
          />
          {isSet && (
            <button type="button" class="btn sm" data-testid={pre + '-secret-clear-' + f.key} aria-pressed={cleared} onClick={() => form.toggleClear(f.key)}>
              {cleared ? t('common.undo') : t('cfg.clear')}
            </button>
          )}
        </div>
      )
      break
    }
    default:
      control = <input id={id} type={f.type === 'url' ? 'url' : 'text'} inputMode={f.type === 'url' ? 'url' : undefined} autocomplete="off" spellcheck={false} data-testid={tid} value={String(v ?? '')} onInput={inp} />
  }
  const bool = f.type === 'boolean'
  return (
    <div class={'field cfg-field' + (bool ? ' cfg-bool' : '')} data-type={f.type}>
      <div class="cfg-head">
        <label for={id}>
          {loc(f.label)}
          {f.required && <span class="req" aria-hidden="true"> *</span>}
        </label>
        {bool && control}
      </div>
      {!bool && control}
      {f.help && <small class="cfg-help">{loc(f.help)}</small>}
      {err && (
        <small class="cfg-err" role="alert" data-testid={pre + '-error-' + f.key}>
          {err}
        </small>
      )}
    </div>
  )
}
