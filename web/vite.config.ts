import { defineConfig } from 'vite'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'
import { existsSync, readdirSync } from 'node:fs'

const __dirname = dirname(fileURLToPath(import.meta.url))

// libsodium-wrappers ESM imports "./libsodium.mjs" from the sibling
// libsodium package. Bun's workspace layout may store it under
// node_modules/.bun/libsodium@<ver>/node_modules/libsodium/...
// rather than a hoisted node_modules/libsodium/...
function resolveLibsodiumMjs(): string {
  const root = resolve(__dirname, '..')
  const candidates = [
    // standard hoisted layout
    resolve(root, 'node_modules/libsodium/dist/modules-esm/libsodium.mjs'),
    // web-local node_modules
    resolve(__dirname, 'node_modules/libsodium/dist/modules-esm/libsodium.mjs'),
  ]

  // bun .bun/<pkg>@<ver>/node_modules/<pkg>/... layout
  const bunDir = resolve(root, 'node_modules/.bun')
  try {
    for (const entry of readdirSync(bunDir)) {
      if (entry.startsWith('libsodium@')) {
        const p = resolve(bunDir, entry, 'node_modules/libsodium/dist/modules-esm/libsodium.mjs')
        candidates.push(p)
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
  base: '/_panel/assets/',
  build: {
    outDir: '../internal/views/web/dist',
    emptyOutDir: true,
    minify: 'esbuild',
    target: 'es2022',
    rollupOptions: {
      input: 'src/panel.ts',
      output: {
        entryFileNames: 'panel.js',
        chunkFileNames: '[name].js',
        assetFileNames: '[name][extname]',
      },
    },
  },
  resolve: {
    alias: {
      './libsodium.mjs': resolveLibsodiumMjs(),
    },
  },
})
