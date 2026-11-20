import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { fileURLToPath, URL } from 'node:url';

// The dev server proxies /api and /login to todayd, so `pnpm --filter
// @today/web dev` runs the real rules against the real database: no mock
// layer, no second copy of what "today" means.
export default defineConfig({
  // Served from the root: Go hands every path it does not own to this app,
  // so index.html's assets must resolve from /.
  base: '/',
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      output: {
        // Four vendor chunks, each cached on its own: React only changes when
        // React changes, the router when the router does. Without this Vite
        // folds every dependency into the entry chunk, and one 556 kB file is
        // both over the size warning and re-downloaded on any app-code edit.
        // The app's own code stays in the entry chunk, which is the number
        // that matters for a first paint.
        // Only React is pinned. Everything else keeps Rollup's own decision,
        // which already puts a library in the chunk that first needs it: the
        // calendar and cmdk stay inside the drawer and palette, so a first
        // paint never pays for them. Folding all of node_modules into one
        // "vendor" chunk does the opposite — it drags the drawer's date
        // picker into the entry, and the gzip total for a first paint grows
        // even though the entry file looks smaller.
        manualChunks(id) {
          if (/node_modules\/(react|react-dom|scheduler)\//.test(id)) return 'react';
          if (/node_modules\/react-router/.test(id)) return 'router';
          if (id.includes('node_modules/@tanstack/')) return 'query';
          if (id.includes('node_modules/@dnd-kit/')) return 'sortable';
          return undefined;
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8787',
      '/login': 'http://127.0.0.1:8787',
      '/logout': 'http://127.0.0.1:8787',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.test.{ts,tsx}'],
  },
});
