import { request } from '@playwright/test'
import { CSRF, expect, origin, test } from '../helpers'

// Runs last on purpose: afterwards this client IP is throttled for a minute.
test('login is rate limited after repeated failures (429)', async ({ api }) => {
  void api
  const anon = await request.newContext({ baseURL: origin(), extraHTTPHeaders: CSRF })
  const codes: number[] = []
  for (let i = 0; i < 7; i++) codes.push((await anon.post('/api/auth/login', { data: { password: 'nope-nope' } })).status())
  expect(codes.slice(0, 5).every((c) => c === 401)).toBe(true)
  expect(codes).toContain(429)
  await anon.dispose()
})
