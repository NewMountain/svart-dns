import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  base: '/',
  resolve: {
    alias: [
      { find: /^apexcharts$/, replacement: 'apexcharts/src/apexcharts.js' },
      { find: './assets/apexcharts.css', replacement: './assets/apexcharts.css?inline' },
    ],
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/@svgdotjs/')) return 'chart-svg';
          if (
            id.includes('node_modules/react-apexcharts') ||
            id.includes('node_modules/apexcharts')
          ) {
            return 'charts';
          }
          if (
            id.includes('node_modules/@codemirror') ||
            id.includes('node_modules/@lezer') ||
            id.includes('node_modules/crelt') ||
            id.includes('node_modules/w3c-keyname')
          ) {
            return 'editor';
          }
          if (
            id.includes('node_modules/react/') ||
            id.includes('node_modules/react-dom/') ||
            id.includes('node_modules/scheduler/') ||
            id.includes('node_modules/react-router') ||
            id.includes('node_modules/@remix-run/')
          ) {
            return 'react-vendor';
          }
        },
      },
    },
  },
  server: {
    port: 3005,
    proxy: {
      '/api': 'http://localhost:3006',
      '/health': 'http://localhost:3006',
      '/metrics': 'http://localhost:3006',
    },
  },
});
