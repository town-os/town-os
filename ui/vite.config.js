import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'
import os from 'os'

const apiTarget = process.env.VITE_API_URL || 'http://localhost:5309'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    allowedHosts: [os.hostname()],
    proxy: {
      '/status': apiTarget,
      '/account': apiTarget,
      '/storage': apiTarget,
      '/repository': apiTarget,
      '/packages': apiTarget,
      '/systemd': apiTarget,
      '/audit': apiTarget,
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.test.{js,jsx}'],
    exclude: ['src/**/*.integration.test.*', 'node_modules'],
  },
})
