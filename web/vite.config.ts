import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

export default defineConfig({
  resolve: { alias: { '@mui/styled-engine': '@mui/styled-engine-sc' } },
  plugins: [react()],
  build: {
    license: { fileName: 'THIRD_PARTY_NOTICES.txt' },
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            { name: 'charts', test: /node_modules\/(@mui\/x-charts|d3-)/ },
            { name: 'react', test: /node_modules\/(react|react-dom|scheduler)\//, priority: 20 },
          ],
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': {
        target: process.env.ARVELD_API_URL ?? 'http://127.0.0.1:8080',
        changeOrigin: false,
      },
      '^/readyz$': {
        target: process.env.ARVELD_API_URL ?? 'http://127.0.0.1:8080',
        changeOrigin: false,
      },
    },
  },
  preview: { port: 4173, strictPort: true },
});
