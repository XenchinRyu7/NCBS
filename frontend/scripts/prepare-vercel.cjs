const fs = require('fs');
const path = require('path');

// Determine cwd and paths
const scriptDir = __dirname;
const frontendDir = path.resolve(scriptDir, '..');
const repoRootDir = path.resolve(frontendDir, '..');

const frontendOutput = path.join(frontendDir, '.vercel', 'output');
const rootOutput = path.join(repoRootDir, '.vercel', 'output');

console.log('[prepare-vercel] Checking Build Output API v3 directories...');

// 1. If frontend/.vercel/output exists and we can access repoRootDir, copy to root .vercel/output
if (fs.existsSync(frontendOutput)) {
  try {
    fs.rmSync(rootOutput, { recursive: true, force: true });
    fs.mkdirSync(path.dirname(rootOutput), { recursive: true });
    fs.cpSync(frontendOutput, rootOutput, { recursive: true });
    console.log('[prepare-vercel] ✓ Synced Build Output API v3 to root .vercel/output');
  } catch (err) {
    console.log('[prepare-vercel] Note: Could not sync to repo root (isolated build context):', err.message);
  }
}

// 2. Inject API Gateway rewrites into all discovered config.json
const targets = [
  path.join(rootOutput, 'config.json'),
  path.join(frontendOutput, 'config.json'),
  path.join(process.cwd(), '.vercel', 'output', 'config.json'),
];

const apiRewrites = [
  { src: '^/v1/(.*)$', dest: 'https://cbs.api.saefulrohman.dev/v1/$1' },
  { src: '^/docs$', dest: 'https://cbs.api.saefulrohman.dev/docs' },
  { src: '^/docs/(.*)$', dest: 'https://cbs.api.saefulrohman.dev/docs/$1' },
  { src: '^/health$', dest: 'https://cbs.api.saefulrohman.dev/v1/system/health' },
  { src: '^/system/(.*)$', dest: 'https://cbs.api.saefulrohman.dev/v1/system/$1' },
];

const touched = new Set();
for (const configPath of targets) {
  if (fs.existsSync(configPath) && !touched.has(configPath)) {
    touched.add(configPath);
    try {
      const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
      if (Array.isArray(config.routes)) {
        const nonApiRoutes = config.routes.filter(
          (r) => !apiRewrites.some((ar) => ar.src === r.src)
        );
        config.routes = [...apiRewrites, ...nonApiRoutes];
        fs.writeFileSync(configPath, JSON.stringify(config, null, 2));
        console.log(`[prepare-vercel] ✓ Injected API rewrites into ${configPath}`);
      }
    } catch (e) {
      console.warn(`[prepare-vercel] Failed to update ${configPath}:`, e.message);
    }
  }
}
console.log('[prepare-vercel] Build Output API v3 preparation complete.');
