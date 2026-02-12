/**
 * Vite Configuration for Web builds
 */
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'
import pkg from './package.json'

export default defineConfig({
  plugins: [react()],
  root: './src/renderer',
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version),
  },
  publicDir: 'public',
  base: '/',
  build: {
    outDir: '../../dist/web',
    emptyOutDir: true,
    sourcemap: false,
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src/renderer'),
      '@shared': path.resolve(__dirname, './shared'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8081',
        changeOrigin: true,
      },
      '/ws': {
        target: 'http://localhost:8081',
        changeOrigin: true,
        ws: true,
      },
    },
  },
})
