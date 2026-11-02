import { spawnSync } from 'node:child_process'
import { existsSync, readFileSync, rmSync } from 'node:fs'
import { stateFile } from './global-setup'

export default async function globalTeardown() {
  if (!existsSync(stateFile)) return
  const { name, dataDir } = JSON.parse(readFileSync(stateFile, 'utf8'))
  spawnSync('docker', ['rm', '-f', name], { stdio: 'ignore' })
  rmSync(dataDir, { recursive: true, force: true })
  rmSync(stateFile, { force: true })
}
