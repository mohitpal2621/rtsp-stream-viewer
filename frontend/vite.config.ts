import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// In development the Go backend runs on :8080 and Vite forwards API and
// WebSocket requests to it, so the frontend can use same-origin URLs.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': { target: 'http://localhost:8080', ws: true },
    },
  },
})
