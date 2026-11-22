import { spawn } from 'node:child_process';
import { mkdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { createHash } from 'node:crypto';
import { createServer as createHTTPServer, request } from 'node:http';
import { createServer as createHTTPSServer } from 'node:https';

const root = resolve(import.meta.dirname, '..');
const command = process.argv[2];
function developmentEnv(secure) {
  const origin = secure ? 'https://localhost:3000' : 'http://localhost:3000';
  return {
    ...process.env,
    BLOG_ENV: 'development',
    BLOG_DATA_ROOT:
      process.env.BLOG_DATA_ROOT ||
      resolve(
        tmpdir(),
        'modawana-dev-' + createHash('sha256').update(root).digest('hex').slice(0, 12),
      ),
    BLOG_LISTEN_ADDR: process.env.BLOG_LISTEN_ADDR || '127.0.0.1:8082',
    BLOG_BASE_URL: origin,
    BLOG_RP_ID: 'localhost',
    BLOG_WEBAUTHN_ORIGINS: origin,
  };
}

function run(bin, args, cwd = root, env = process.env) {
  return new Promise((resolve, reject) => {
    const child = spawn(bin, args, { cwd, env, stdio: 'inherit' });
    child.once('error', reject);
    child.once('exit', (code) =>
      code === 0 ? resolve() : reject(new Error(`${bin} exited ${code}`)),
    );
  });
}
async function startDevelopment(secure) {
  const env = developmentEnv(secure);
  const options = {};
  if (secure) {
    if (!env.BLOG_DEV_TLS_CERT || !env.BLOG_DEV_TLS_KEY) {
      throw new Error(
        'Set BLOG_DEV_TLS_CERT and BLOG_DEV_TLS_KEY to trusted localhost certificate files. See README.md.',
      );
    }
    options.cert = readFileSync(env.BLOG_DEV_TLS_CERT);
    options.key = readFileSync(env.BLOG_DEV_TLS_KEY);
  }
  const upstream = new URL('http://' + env.BLOG_LISTEN_ADDR);
  const proxy = (secure ? createHTTPSServer : createHTTPServer)(options, (incoming, outgoing) => {
    if (!['localhost:3000', '127.0.0.1:3000'].includes(incoming.headers.host?.toLowerCase())) {
      outgoing.writeHead(403).end();
      return;
    }
    const backend = request(
      upstream,
      { method: incoming.method, path: incoming.url, headers: incoming.headers },
      (response) => {
        outgoing.writeHead(response.statusCode, response.headers);
        response.on('error', () => outgoing.destroy());
        response.pipe(outgoing);
      },
    );
    backend.on('error', () => {
      if (outgoing.headersSent) {
        outgoing.destroy();
      } else {
        outgoing.writeHead(502).end('Development server unavailable');
      }
    });
    outgoing.on('close', () => backend.destroy());
    incoming.pipe(backend);
  });
  await run('npm', ['run', 'build'], resolve(root, 'frontend'));
  mkdirSync(resolve(root, 'build'), { recursive: true });
  await run('go', ['build', '-o', '../build/blog-dev', './cmd/blog'], resolve(root, 'backend'));

  await new Promise((resolve, reject) => {
    proxy.once('error', reject);
    proxy.listen(3000, '127.0.0.1', resolve);
  });
  console.log(`Development URL: ${env.BLOG_BASE_URL}/en`);
  const backend = spawn(resolve(root, 'build/blog-dev'), [], {
    cwd: resolve(root, 'backend'),
    env,
    stdio: 'inherit',
  });
  const stop = () => backend.kill('SIGTERM');
  process.once('SIGINT', stop);
  process.once('SIGTERM', stop);
  try {
    await new Promise((resolve, reject) => {
      backend.once('error', reject);
      backend.once('exit', (code) =>
        code ? reject(new Error(`blog-dev exited ${code}`)) : resolve(),
      );
    });
  } finally {
    proxy.close();
    proxy.closeAllConnections();
    process.off('SIGINT', stop);
    process.off('SIGTERM', stop);
  }
}

try {
  switch (command) {
    case 'install':
      await run('npm', ['ci', ...process.argv.slice(3)], resolve(root, 'frontend'));
      break;
    case 'build':
      await run('npm', ['run', 'build'], resolve(root, 'frontend'));
      mkdirSync(resolve(root, 'build'), { recursive: true });
      await run(
        'go',
        ['build', '-trimpath', '-o', '../build/blog', './cmd/blog'],
        resolve(root, 'backend'),
      );
      await run(
        'go',
        ['build', '-trimpath', '-o', '../build/publisher', './cmd/publisher'],
        resolve(root, 'backend'),
      );
      break;
    case 'lint':
      await run('node', ['scripts/quality.mjs', 'lint']);
      break;
    case 'format':
      await run('node', ['scripts/quality.mjs', 'lint', '--fix']);
      await run('node', ['scripts/quality.mjs', 'format', '--fix']);
      break;
    case 'check':
      await run('node', ['scripts/quality.mjs', 'lint']);
      await run('node', ['scripts/quality.mjs', 'format']);
      await run('node', ['scripts/check-docs.mjs']);
      await run('node', [
        '--test',
        'scripts/publish.test.mjs',
        'scripts/smoke-policy.test.mjs',
        'scripts/quality.test.mjs',
      ]);
      await run('node', ['scripts/build-label-registry.mjs', '--check']);
      await run('node', ['scripts/build-palette-vectors.mjs', '--check']);
      await run('node', ['design-system/scripts/build-tokens.mjs', '--check']);
      await run('node', ['ui/scripts/build-tokens.mjs', '--check']);
      if (
        !readFileSync(resolve(root, 'design-system/components.css')).equals(
          readFileSync(resolve(root, 'ui/src/styles/components.css')),
        )
      ) {
        throw new Error('Component styles differ');
      }
      await run('npm', ['run', 'check'], resolve(root, 'frontend'));
      await run('node', ['scripts/markdown-parity.mjs']);
      await run(
        'go',
        ['test', ...(process.argv.includes('--race') ? ['-race'] : []), './...'],
        resolve(root, 'backend'),
      );
      await run('go', ['vet', './...'], resolve(root, 'backend'));
      break;
    case 'bootstrap':
    case 'bootstrap:https':
    case 'recover':
    case 'recover:https':
      await run(
        'go',
        ['run', './cmd/blog', command.startsWith('recover') ? 'recover' : 'bootstrap'],
        resolve(root, 'backend'),
        developmentEnv(command.endsWith(':https')),
      );
      break;
    case 'dev':
    case 'dev:https':
      await startDevelopment(command === 'dev:https');
      break;
    default:
      throw new Error(
        'Usage: node scripts/project.mjs install|build|check|dev|dev:https|bootstrap|bootstrap:https|recover|recover:https|lint|format',
      );
  }
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
