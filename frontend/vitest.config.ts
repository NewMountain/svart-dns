import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  // Contract examples live alongside the Go schema sources.
  server: { fs: { allow: ['..'] } },
  test: {
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/**/*.test.{ts,tsx}', 'src/test/**'],
      reporter: ['text', 'json', 'json-summary', 'html'],
      thresholds: { statements: 80, branches: 80, functions: 80, lines: 80 },
    },
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    css: true,
    clearMocks: true,
    mockReset: true,
    restoreMocks: true,
  },
});
