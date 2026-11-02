import { request } from '@playwright/test'
import { CSRF, PASSWORD, expect, openApp, openSettings, origin, test } from '../helpers'

test('change the admin password in Settings > Security; other sessions are invalidated', async ({ page, admin }) => {
  const NEW = 'brand-new-pass-9'
  await openApp(page)
  try {
    await openSettings(page)
    await page.getByTestId('settings-tab-security').click()
    // wrong old password
    await page.getByTestId('pw-old').fill('not-my-password')
    await page.getByTestId('pw-new').fill(NEW)
    await page.getByTestId('pw-save').click()
    await expect(page.getByTestId('pw-error')).toBeVisible()
    // too short
    await page.getByTestId('pw-old').fill(PASSWORD)
    await page.getByTestId('pw-new').fill('123')
    await page.getByTestId('pw-save').click()
    await expect(page.getByTestId('pw-error')).toBeVisible()
    // success
    await page.getByTestId('pw-old').fill(PASSWORD)
    await page.getByTestId('pw-new').fill(NEW)
    await page.getByTestId('pw-save').click()
    await expect(page.getByTestId('toast').filter({ hasText: /./ }).first()).toHaveAttribute('data-kind', 'success')

    const anon = await request.newContext({ baseURL: origin(), extraHTTPHeaders: CSRF })
    expect((await anon.post('/api/auth/login', { data: { password: PASSWORD } })).status()).toBe(401)
    expect((await anon.post('/api/auth/login', { data: { password: NEW } })).status()).toBe(200)
    await anon.dispose()
    // the other (old) session is dead, the current browser session was re-issued and still works
    expect((await admin.get('/api/export')).status()).toBe(401)
    await page.reload()
    await expect(page.getByTestId('edit-toggle')).toBeVisible()
  } finally {
    // restore the shared password for the rest of the suite
    const r = await page.request.post('/api/auth/password', { headers: CSRF, data: { old_password: NEW, new_password: PASSWORD } })
    if (!r.ok()) await page.request.post('/api/auth/password', { headers: CSRF, data: { old_password: PASSWORD, new_password: PASSWORD } })
    expect((await admin.post('/api/auth/login', { headers: CSRF, data: { password: PASSWORD } })).ok()).toBe(true)
  }
})
