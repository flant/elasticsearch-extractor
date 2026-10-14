import { defineConfig, loadEnv, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'

// Две страницы, как их отдаёт backend: / - снапшоты, /search/ - поиск.
// Сборка идёт в dist/ui, чтобы закоммиченный dist/.gitkeep пережил её.
// API, выгрузки в режиме разработки проксируются на Go-backend: BACKEND=http://host:port.

// В режиме разработки /search/ ведёт на search.html, как в backend.
const pages: Plugin = {
  name: 'xtractor-pages',
  configureServer(server) {
    server.middlewares.use((req, _res, next) => {
      // типов Node в проекте нет, у запроса нужен только url
      const r = req as unknown as { url?: string }
      const url = r.url ?? ''
      if (url === '/search' || url.startsWith('/search/') || url.startsWith('/search?')) {
        r.url = '/search.html' + (url.includes('?') ? url.slice(url.indexOf('?')) : '')
      }
      next()
    })
  },
}

export default defineConfig(({ mode }) => {
  const backend = loadEnv(mode, '.', '').BACKEND || 'http://127.0.0.1:9400'
  return {
    plugins: [vue(), pages],
    build: {
      outDir: 'dist/ui',
      emptyOutDir: true,
      target: 'es2022',
      rollupOptions: {
        input: { index: 'index.html', search: 'search.html' },
      },
    },
    server: {
      // всё, что отдаёт extractor, а не UI: API, выгрузки
      proxy: { '/api': backend, '/data': backend },
    },
  }
})
