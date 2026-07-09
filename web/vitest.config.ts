import { defineConfig } from 'vitest/config'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'
import { existsSync, readdirSync } from 'node:fs'

const __dirname = dirname(fileURLToPath(import.meta.url))

function resolveLibsodiumMjs(): string {
  const root = resolve(__dirname, '..')
  const candidates = [
    resolve(root, 'node_modules/libsodium/dist/modules-esm/libsodium.mjs'),
    resolve(__dirname, 'node_modules/libsodium/dist/modules-esm/libsodium.mjs'),
  ]
  const bunDir = resolve(root, 'node_modules/.bun')
  try {
    for (const entry of readdirSync(bunDir)) {
      if (entry.startsWith('libsodium@')) {
        candidates.push(resolve(bunDir, entry, 'node_modules/libsodium/dist/modules-esm/libsodium.mjs'))
        break
      }
    }
  } catch {}
  for (const p of candidates) {
    if (existsSync(p)) return p
  }
  throw new Error('Could not find libsodium.mjs — run `bun install` first')
}

export default defineConfig({
  test: {
    name: 's3-server',
    include: ['src/**/*.test.ts'],
    setupFiles: ['./src/__tests__/setup.ts'],
    environment: 'happy-dom',
    passWithNoTests: true,
  },
  resolve: {
    alias: {
      './libsodium.mjs': resolveLibsodiumMjs(),
    },
  },
})
