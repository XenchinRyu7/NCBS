import { defineConfig } from 'astro/config';
import node from '@astrojs/node';
import tailwind from '@astrojs/tailwind';
import react from '@astrojs/react';

// https://astro.build/config
export default defineConfig({
  output: 'server',
  adapter: node({
    mode: 'standalone',
  }),
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
  },
  server: {
    port: 3000,
    host: true,
  },
});
