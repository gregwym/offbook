import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// Separate from vite.config.ts (which loads the Tailwind plugin and dev
// proxy) — tests never render actual Tailwind CSS output or hit a real
// backend, so neither is needed here.
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    globals: false,
  },
})
