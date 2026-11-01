import { readdir, readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const root = resolve(import.meta.dirname, '../..');
const patterns = [
  /-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----/,
  /(?:ghp_|github_pat_|sk_live_)[A-Za-z0-9_]{20,}/,
  /(?:BLOG_AUTH_KEY|BLOG_PUBLISHER_TOKEN|NOTION_API_KEY)\s*[=:]\s*["']?[A-Za-z0-9_+/=-]{32,}/,
];
async function walk(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    if (directory === root && entry.name === 'internal') {
      continue;
    }
    if (['.git', 'node_modules', 'test-results', '.test-output', 'build'].includes(entry.name)) {
      continue;
    }
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) {
      await walk(path);
    } else if (/\.(?:[cm]?js|tsx?|json|html|css|go|ya?ml|md|env)$/.test(entry.name)) {
      const text = await readFile(path, 'utf8');
      if (patterns.some((pattern) => pattern.test(text))) {
        throw new Error('Potential credential in ' + path.replace(root + '/', ''));
      }
    }
  }
}
await walk(root);
console.log('Source and built-browser credential-pattern scan passed; no values printed.');
