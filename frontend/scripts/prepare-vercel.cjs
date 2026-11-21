const fs = require('fs');
const path = require('path');

// Determine cwd and paths
const scriptDir = __dirname;
const frontendDir = path.resolve(scriptDir, '..');
const repoRootDir = path.resolve(frontendDir, '..');

const frontendOutput = path.join(frontendDir, '.vercel', 'output');
const rootOutput = path.join(repoRootDir, '.vercel', 'output');

console.log('[prepare-vercel] Initializing Vercel Build Output API v3 preparation...');

// Helper: replace all symlinks with actual physical copies
function replaceSymlinksWithRealFiles(dir) {
  if (!fs.existsSync(dir)) return;
  const entries = fs.readdirSync(dir, { withFileTypes: true });
  for (const entry of entries) {
    const fullPath = path.join(dir, entry.name);
    try {
      if (entry.isSymbolicLink()) {
        const realTarget = fs.realpathSync(fullPath);
        fs.unlinkSync(fullPath);
        const stat = fs.statSync(realTarget);
        if (stat.isDirectory()) {
          fs.cpSync(realTarget, fullPath, { recursive: true });
        } else {
          fs.copyFileSync(realTarget, fullPath);
        }
      } else if (entry.isDirectory()) {
        replaceSymlinksWithRealFiles(fullPath);
      }
    } catch (err) {
      // Continue on unresolvable reference
    }
  }
}

// 1. If frontend/.vercel/output exists and we can access repoRootDir, sync to root .vercel/output
if (fs.existsSync(frontendOutput)) {
  try {
    fs.rmSync(rootOutput, { recursive: true, force: true });
    fs.mkdirSync(path.dirname(rootOutput), { recursive: true });
    fs.cpSync(frontendOutput, rootOutput, { recursive: true });
    console.log('[prepare-vercel] ✓ Synced Build Output API v3 to root .vercel/output');
  } catch (err) {
    console.log('[prepare-vercel] Note: Could not sync to repo root:', err.message);
  }
}

// 2. Resolve all pnpm absolute symlinks in functions to physical directories and create root index.mjs handler
const functionDirs = [
  path.join(rootOutput, 'functions'),
  path.join(frontendOutput, 'functions'),
];

for (const fDir of functionDirs) {
  if (fs.existsSync(fDir)) {
    const funcs = fs.readdirSync(fDir, { withFileTypes: true });
    for (const f of funcs) {
      if (f.isDirectory() && f.name.endsWith('.func')) {
        const funcPath = path.join(fDir, f.name);
        replaceSymlinksWithRealFiles(funcPath);
        console.log(`[prepare-vercel] ✓ Dereferenced all symlinks inside ${f.name}`);

        const vcConfigFile = path.join(funcPath, '.vc-config.json');
        if (fs.existsSync(vcConfigFile)) {
          try {
            const vcConfig = JSON.parse(fs.readFileSync(vcConfigFile, 'utf8'));
            const originalHandler = vcConfig.handler || 'frontend/dist/server/entry.mjs';

            // Create resilient root index.mjs wrapper for Lambda launcher
            const indexFile = path.join(funcPath, 'index.mjs');
            const handlerImportPath = originalHandler.startsWith('.') ? originalHandler : `./${originalHandler}`;
            const wrapperContent = `import ssrEntry from '${handlerImportPath}';

export default async function handler(req, res) {
  try {
    return await ssrEntry(req, res);
  } catch (err) {
    console.error('[NCBS-SSR-CRASH]', err);
    res.statusCode = 500;
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({
      error: 'NCBS Serverless Function Error',
      message: err?.message,
      stack: err?.stack,
    }));
  }
}
`;
            fs.writeFileSync(indexFile, wrapperContent);

            vcConfig.handler = 'index.mjs';
            vcConfig.runtime = 'nodejs24.x';
            fs.writeFileSync(vcConfigFile, JSON.stringify(vcConfig, null, 2));
            console.log(`[prepare-vercel] ✓ Set clean root index.mjs handler and nodejs24.x in ${vcConfigFile}`);
          } catch (e) {
            console.warn('[prepare-vercel] Could not patch .vc-config.json:', e.message);
          }
        }
      }
    }
  }
}

// 3. Inject API Gateway rewrites into all discovered config.json
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
