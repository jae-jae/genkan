import { defineConfig } from 'vite'

const backend = (globalThis as any).process?.env?.GENKAN_BACKEND || 'http://localhost:8080'

export default defineConfig({
  esbuild: { jsx: 'automatic', jsxImportSource: 'preact' },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: backend, changeOrigin: false },
      '/uploads': backend,
      '/mcp': backend,
      '/healthz': backend,
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2020',
    cssCodeSplit: false,
    modulePreload: { polyfill: false },
    assetsInlineLimit: 0,
    reportCompressedSize: true,
  },
})
