import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// The Mac app loads this build from disk (mygo's frontendDist), so the base
// must be relative: file URLs have no origin to resolve "/assets/…" against.
export default defineConfig({
  plugins: [svelte()],
  base: './',
  // Browser conditions first: without them `mount` resolves to svelte/server
  // and every component test fails with lifecycle_function_unavailable.
  resolve: { conditions: ['browser'] },
  build: { outDir: 'dist', emptyOutDir: true, sourcemap: false, target: 'safari18' },
  test: { environment: 'jsdom', globals: true, include: ['src/**/*.test.ts'] },
});
