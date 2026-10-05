import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The dev server proxies the WebSocket endpoint and the small HTTP API to the
// Go backend so that a single origin (and therefore a single ngrok tunnel)
// serves the whole Telegram Mini App during development.
const BACKEND = process.env.VITE_BACKEND_URL ?? 'http://localhost:8080';

export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    // ngrok / Telegram open the app from an arbitrary public host name.
    allowedHosts: true,
    proxy: {
      '/ws': { target: BACKEND, ws: true, changeOrigin: true },
      '/api': { target: BACKEND, changeOrigin: true },
      '/healthz': { target: BACKEND, changeOrigin: true },
    },
  },
  preview: {
    host: '0.0.0.0',
    port: 4173,
    allowedHosts: true,
    proxy: {
      '/ws': { target: BACKEND, ws: true, changeOrigin: true },
      '/api': { target: BACKEND, changeOrigin: true },
      '/healthz': { target: BACKEND, changeOrigin: true },
    },
  },
  build: {
    target: 'es2022',
    sourcemap: false,
  },
});
