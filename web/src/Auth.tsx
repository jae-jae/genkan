import { useState } from 'preact/hooks'
import { Logo } from './icons'
import { t } from './i18n'
import { login, role, setup, settings, showLogin } from './store'

export function AuthScreen({ mode }: { mode: 'login' | 'setup' }) {
  const [pw, setPw] = useState('')
  const [pw2, setPw2] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const isSetup = mode === 'setup'
  const submit = async (e: Event) => {
    e.preventDefault()
    setErr('')
    if (isSetup) {
      if (pw.length < 6) return setErr(t('auth.tooShort'))
      if (pw !== pw2) return setErr(t('auth.mismatch'))
    }
    setBusy(true)
    try {
      await (isSetup ? setup(pw) : login(pw))
    } catch (er: any) {
      setErr(er?.status === 401 ? t('auth.wrong') : er?.status === 429 ? t('auth.tooMany') : er?.status === 0 ? t('err.network') : er?.message || t('err.generic'))
    }
    setBusy(false)
  }
  return (
    <div class="auth">
      <form class="auth-card glass" onSubmit={submit} data-testid={isSetup ? 'setup-form' : 'login-form'}>
        <div class="auth-logo">
          <Logo size={60} />
        </div>
        <h1>{settings.value.title || 'Genkan'}</h1>
        <p class="auth-sub">{isSetup ? t('auth.setupHint') : t('auth.loginHint')}</p>
        <div class="field">
          <label for="a-pw">{isSetup ? t('auth.newPassword') : t('auth.password')}</label>
          <input id="a-pw" type="password" autocomplete={isSetup ? 'new-password' : 'current-password'} data-testid={isSetup ? 'setup-password' : 'login-password'} value={pw} autofocus onInput={(e) => setPw((e.target as HTMLInputElement).value)} />
        </div>
        {isSetup && (
          <div class="field">
            <label for="a-pw2">{t('auth.confirm')}</label>
            <input id="a-pw2" type="password" autocomplete="new-password" data-testid="setup-confirm" value={pw2} onInput={(e) => setPw2((e.target as HTMLInputElement).value)} />
          </div>
        )}
        {err && (
          <p class="form-error" role="alert" data-testid="auth-error">
            {err}
          </p>
        )}
        <button class="btn primary block" type="submit" disabled={busy || !pw} data-testid={isSetup ? 'setup-submit' : 'login-submit'}>
          {isSetup ? t('auth.setupBtn') : t('auth.loginBtn')}
        </button>
        {role.value === 'guest' && !isSetup && (
          <button type="button" class="linkbtn center" data-testid="login-back" onClick={() => (showLogin.value = false)}>
            {t('auth.back')}
          </button>
        )}
      </form>
    </div>
  )
}
