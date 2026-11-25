import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { generateKeyPairSync } from 'node:crypto';
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { verifyPackage } from './verify-package.mjs';

const input = resolve(process.argv[2] || 'build/releases/acceptance.tar.gz');
const expectedCommit = process.argv[3];
const work = await mkdtemp(resolve(tmpdir(), 'blog-catalog-smoke-'));
try {
  const { source, manifest } = await verifyPackage(input, work);
  if (expectedCommit) {
    assert.equal(manifest.source_commit, expectedCommit);
    assert.equal(manifest.source_dirty, false);
  }

  const examples = resolve(source, 'examples');
  const catalog = resolve(work, 'catalog');
  await mkdir(catalog);
  const specFile = resolve(catalog, 'catalog-spec.json');
  await cp(resolve(examples, 'catalog-spec.json'), specFile);
  await cp(resolve(examples, 'themes'), resolve(catalog, 'themes'), { recursive: true });
  execFileSync(
    'zip',
    ['-X', resolve(catalog, 'plugin-example.zip'), 'manifest.json', 'plugin.wasm'],
    { cwd: resolve(examples, 'plugins/example') },
  );

  const { privateKey, publicKey } = generateKeyPairSync('ed25519', {
    privateKeyEncoding: { type: 'pkcs8', format: 'pem' },
    publicKeyEncoding: { type: 'spki', format: 'pem' },
  });
  const privateFile = resolve(work, 'private.pem');
  const publicFile = resolve(work, 'public.pem');
  await writeFile(privateFile, privateKey, { mode: 0o600 });
  await writeFile(publicFile, publicKey);
  const tool = resolve(source, 'scripts/release/catalog.mjs');
  const expiry = new Date(Date.now() + 86400000).toISOString().replace(/\.\d{3}Z$/, 'Z');
  const index = resolve(work, 'catalog.json');
  const signature = resolve(work, 'catalog.sig.json');
  const keyring = resolve(work, 'trusted-keys.json');
  execFileSync(process.execPath, [tool, 'index', specFile, index, expiry]);
  execFileSync(process.execPath, [tool, 'sign', index, privateFile, 'smoke-test', signature]);
  execFileSync(process.execPath, [tool, 'keyring', publicFile, 'smoke-test', keyring]);

  const binary = resolve(source, 'blog');
  const options = {
    cwd: source,
    encoding: 'utf8',
    env: {
      ...process.env,
      BLOG_ENV: 'production',
      BLOG_BASE_URL: 'https://catalog-smoke.test',
      BLOG_RP_ID: 'catalog-smoke.test',
      BLOG_WEBAUTHN_ORIGINS: 'https://catalog-smoke.test',
      BLOG_DATA_ROOT: resolve(work, 'data'),
      BLOG_FRONTEND_DIST: resolve(source, 'frontend'),
      BLOG_TRUSTED_PROXIES: '',
    },
  };
  const spec = JSON.parse(await readFile(specFile, 'utf8'));
  const entryArgs = (item) => [
    '--catalog',
    index,
    '--signature',
    signature,
    '--keyring',
    keyring,
    '--bundle',
    resolve(catalog, item.bundle),
    '--kind',
    item.kind,
    '--id',
    item.id,
    '--version',
    item.version,
  ];
  for (const item of spec.entries) {
    const args = entryArgs(item);
    const preview = () =>
      JSON.parse(execFileSync(binary, ['extension', 'preview', ...args], options));
    const review = preview();
    assert.equal(review.entry.id, item.id);
    assert.match(review.review_sha256, /^[0-9a-f]{64}$/);
    execFileSync(
      binary,
      ['extension', 'install', ...args, '--approve', review.review_sha256],
      options,
    );
    const approved = preview();
    if (item.kind === 'plugin') {
      assert.equal(approved.plugin.current.version, item.version);
      assert.equal(approved.plugin.enabled, false);
    } else {
      assert.equal(approved.current_theme.checksum, review.entry.sha256);
    }
  }
  const trusted = JSON.parse(await readFile(keyring, 'utf8'));
  trusted.revoked.push('smoke-test');
  await writeFile(keyring, JSON.stringify(trusted));
  const rejected = spawnSync(
    binary,
    ['extension', 'preview', ...entryArgs(spec.entries[0])],
    options,
  );
  assert.equal(rejected.status, 1);
  assert.match(rejected.stderr, /catalog signature, entry or bundle rejected/);
  console.log(
    'Packaged catalog tools and CLI installed the curated samples and rejected the revoked key.',
  );
} finally {
  await rm(work, { recursive: true, force: true });
}
