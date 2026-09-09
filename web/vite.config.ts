import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  // The Go binary embeds web/dist and serves it at the root of the
  // console listener, which does nothing else.
  base: '/',
  build: {
    rollupOptions: {
      output: {
        // CodeMirror is the one heavyweight dependency. Its own chunk,
        // so the projects page does not pay for the editor.
        advancedChunks: {
          groups: [
            {
              name: 'codemirror',
              test: /[\\/]node_modules[\\/](?:@codemirror[\\/]|codemirror[\\/]|@lezer[\\/])/,
            },
          ],
        },
      },
    },
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      // The API and the event stream come from `ihttp serve` on :8081.
      '/api': {
        target: 'http://127.0.0.1:8081',
        changeOrigin: true,
      },
    },
  },
})
