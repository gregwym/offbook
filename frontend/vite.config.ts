import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // VITE_PROXY_TARGET points at the backend host. On the host it's localhost;
  // inside docker-compose.dev it's set to http://backend:8000.
  const env = loadEnv(mode, process.cwd(), '')
  const proxyTarget = env.VITE_PROXY_TARGET || 'http://localhost:8000'
  return {
    plugins: [react(), tailwindcss()],
    server: {
      port: 5173,
      // Allow Tailscale MagicDNS (*.ts.net) and Bonjour (*.local) so dev server
      // is reachable from phones/tablets without disabling Vite's host check.
      allowedHosts: ['.ts.net', '.local'],
      proxy: {
        '/api': {
          target: proxyTarget,
          changeOrigin: true,
        },
      },
    },
  }
})
