import { defineConfig } from 'astro/config';
import vercel from '@astrojs/vercel';
import tailwind from '@astrojs/tailwind';
import react from '@astrojs/react';

// https://astro.build/config
export default defineConfig({
  output: 'server',
  adapter: vercel(),
  integrations: [
    tailwind({
      applyBaseStyles: false,
    }),
    react({
      include: ['**/components/**/*.{jsx,tsx}', '**/blocks/**/*.{jsx,tsx}'],
    }),
  ],
  vite: {
    optimizeDeps: {
      include: ['@etherealui/react', 'lucide-react', 'react', 'react-dom'],
    },
    server: {
      proxy: {
        '/v1': {
          target: 'http://localhost:8080',
          changeOrigin: true,
        },
        '/docs': {
          target: 'http://localhost:8080',
          changeOrigin: true,
        },
      },
    },
  },
  server: {
    port: 3000,
    host: true,
  },
});
