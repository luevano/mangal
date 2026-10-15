import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueJsx from '@vitejs/plugin-vue-jsx'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue(), vueJsx()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  },
  server: {
    // `npm run dev` against a local `mangal serve --web` (MANGAL_SERVE=http://127.0.0.1:6977)
    proxy: {
      '/api': { target: process.env.MANGAL_SERVE ?? 'http://127.0.0.1:6977', changeOrigin: true }
    }
  }
})
