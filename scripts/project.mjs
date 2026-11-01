import { spawn } from 'node:child_process';
import { mkdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { createHash } from 'node:crypto';
import { createSecureContext } from 'node:tls';

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
        'personal-blog-dev-' + createHash('sha256').update(root).digest('hex').slice(0, 12),
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
    case 'dev:https': {
      const secure = command === 'dev:https';
      const env = developmentEnv(secure);
      if (secure) {
        if (!env.BLOG_DEV_TLS_CERT || !env.BLOG_DEV_TLS_KEY) {
          throw new Error(
            'Set BLOG_DEV_TLS_CERT and BLOG_DEV_TLS_KEY to trusted localhost certificate files. See README.md.',
          );
        }
        createSecureContext({
          cert: readFileSync(env.BLOG_DEV_TLS_CERT),
          key: readFileSync(env.BLOG_DEV_TLS_KEY),
        });
      }
      console.log(`Development URL: ${env.BLOG_BASE_URL}/en`);
      const children = [
        spawn('go', ['run', './cmd/blog'], {
          cwd: resolve(root, 'backend'),
          stdio: 'inherit',
          env,
        }),
        spawn('npm', ['run', 'dev'], {
          cwd: resolve(root, 'frontend'),
          stdio: 'inherit',
          env: { ...env, BLOG_DEV_HTTPS: secure ? '1' : '0' },
        }),
      ];
      const stop = () => children.forEach((child) => child.kill('SIGTERM'));
      process.on('SIGINT', stop);
      process.on('SIGTERM', stop);
      children.forEach((child) => {
        child.on('error', stop);
        child.on('exit', (code) => {
          if (code) {
            process.exitCode = code;
          }
          stop();
        });
      });
      break;
    }
    default:
      throw new Error(
        'Usage: node scripts/project.mjs install|build|check|dev|dev:https|bootstrap|bootstrap:https|recover|recover:https|lint|format',
      );
  }
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
