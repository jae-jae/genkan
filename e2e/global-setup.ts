import { spawnSync } from 'node:child_process'
import { mkdirSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:net'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { LAN_HOSTS } from './fixtures/homelab'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
export const stateFile = resolve(root, '.tmp', 'e2e-state.json')

function freePort(): Promise<number> {
  return new Promise((res, rej) => {
    const s = createServer()
    s.once('error', rej)
    s.listen(0, '127.0.0.1', () => {
      const p = (s.address() as { port: number }).port
      s.close(() => res(p))
    })
  })
}

function docker(args: string[], opts: { inherit?: boolean } = {}) {
  const r = spawnSync('docker', args, { encoding: 'utf8', stdio: opts.inherit ? ['ignore', 'inherit', 'inherit'] : 'pipe' })
  if (r.status !== 0) throw new Error(`docker ${args.join(' ')} failed:\n${r.stdout ?? ''}${r.stderr ?? ''}`)
  return (r.stdout ?? '').trim()
}

export default async function globalSetup() {
  // Targeting an already running instance: nothing to start.
  if (process.env.GENKAN_E2E_URL) {
    process.env.GENKAN_E2E_URL = process.env.GENKAN_E2E_URL.replace(/\/+$/, '')
    // Links that the *server* has to reach (favicon fetch, health checks) must use an address valid from the
    // server's point of view. For an external instance that is the same URL unless overridden.
    process.env.GENKAN_E2E_SELF ||= process.env.GENKAN_E2E_URL
    return
  }

  const image = process.env.GENKAN_E2E_IMAGE || 'genkan:e2e'
  if (!process.env.GENKAN_E2E_IMAGE) {
    console.log(`[e2e] building ${image} ...`)
    docker(['build', '-t', image, root], { inherit: !!process.env.GENKAN_E2E_VERBOSE })
  }

  const port = await freePort()
  const id = `${process.pid}-${port}`
  const name = `genkan-e2e-${id}`
  const dataDir = resolve(root, '.tmp', `e2e-data-${id}`)
  mkdirSync(dataDir, { recursive: true })

  // *.lan names used by the screenshot dataset resolve to the container itself (so health checks can be up or down).
  const hosts = LAN_HOSTS.flatMap((h) => ['--add-host', `${h}:127.0.0.1`])
  docker(['run', '-d', '--name', name, '-p', `127.0.0.1:${port}:8080`, '-v', `${dataDir}:/data`, ...hosts, image])
  writeFileSync(stateFile, JSON.stringify({ name, dataDir }))

  const url = `http://127.0.0.1:${port}`
  const deadline = Date.now() + 30_000
  for (;;) {
    try {
      const r = await fetch(`${url}/healthz`)
      if (r.ok) break
    } catch {
      /* not up yet */
    }
    if (Date.now() > deadline) {
      const logs = spawnSync('docker', ['logs', name], { encoding: 'utf8' })
      throw new Error(`container ${name} did not become ready\n${logs.stdout}${logs.stderr}`)
    }
    await new Promise((r) => setTimeout(r, 150))
  }
  console.log(`[e2e] container ${name} ready at ${url}`)
  process.env.GENKAN_E2E_URL = url
  // Inside the container the server listens on :8080.
  process.env.GENKAN_E2E_SELF = 'http://127.0.0.1:8080'
}
