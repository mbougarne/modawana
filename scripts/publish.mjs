import { spawnSync } from 'node:child_process';
import { accessSync, closeSync, constants, fstatSync, openSync, readFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseEnv } from 'node:util';

const root = resolve(import.meta.dirname, '..');
const allowed = new Set(['BLOG_URL', 'BLOG_PUBLISHER_TOKEN', 'BLOG_PUBLISHER_CA']);

function loadPublisherConfig(file) {
  let descriptor;
  try {
    descriptor = openSync(file, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
    const info = fstatSync(descriptor);
    if (
      !info.isFile() ||
      info.size > 16384 ||
      (info.mode & 0o077) !== 0 ||
      (process.getuid && info.uid !== process.getuid())
    ) {
      throw new Error();
    }
    const config = parseEnv(readFileSync(descriptor, 'utf8'));
    if (
      Object.keys(config).some((key) => !allowed.has(key)) ||
      !config.BLOG_PUBLISHER_TOKEN ||
      /[\r\n]/.test(config.BLOG_PUBLISHER_TOKEN) ||
      config.BLOG_PUBLISHER_TOKEN === 'PASTE_YOUR_BLOG_TOKEN_HERE'
    ) {
      throw new Error();
    }
    const origin = new URL(config.BLOG_URL);
    if (
      origin.protocol !== 'https:' ||
      origin.username ||
      origin.password ||
      origin.pathname !== '/' ||
      origin.search ||
      origin.hash
    ) {
      throw new Error();
    }
    return config;
  } catch {
    throw new Error(
      'Publisher configuration rejected. Use an owner-only regular file (chmod 600), an HTTPS BLOG_URL and a nonempty BLOG_PUBLISHER_TOKEN. Only BLOG_PUBLISHER_CA is optional.',
    );
  } finally {
    if (descriptor !== undefined) {
      closeSync(descriptor);
    }
  }
}

function runPublisher(
  args,
  {
    environment = process.env,
    file = environment.BLOG_PUBLISHER_CONFIG ||
      resolve(homedir(), '.config/personal-blog/publisher.env'),
    binary = resolve(root, 'build/publisher'),
    stdio = 'inherit',
  } = {},
) {
  const config = loadPublisherConfig(file);
  try {
    accessSync(binary, constants.X_OK);
  } catch {
    throw new Error(
      'Publisher executable unavailable. Build backend/cmd/publisher into build/publisher.',
    );
  }
  if (args.length === 1 && args[0] === '--check') {
    console.log('Publisher configuration and executable are ready. No network request was sent.');
    return 0;
  }
  const result = spawnSync(binary, args, {
    env: { ...environment, BLOG_PUBLISHER_CA: '', ...config },
    stdio,
  });
  if (result.error) {
    throw new Error('Publisher executable could not start.');
  }
  return result.status ?? 1;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    process.exitCode = runPublisher(process.argv.slice(2));
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}

export { loadPublisherConfig, runPublisher };
