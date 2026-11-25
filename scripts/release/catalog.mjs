import { createHash, createPrivateKey, createPublicKey, sign } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';

const [command, ...args] = process.argv.slice(2);
const keyID = /^[a-z0-9][a-z0-9-]{0,63}$/;
const writeJSON = (path, value) =>
  writeFile(path, JSON.stringify(value, null, 2) + '\n', { flag: 'wx' });

if (command === 'index' && args.length === 3) {
  const [specFile, output, expiresAt] = args;
  if (
    !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$/.test(expiresAt) ||
    !Number.isFinite(Date.parse(expiresAt))
  ) {
    throw new Error('Expected expiry in UTC RFC 3339 format');
  }
  const spec = JSON.parse(await readFile(specFile, 'utf8'));
  const entries = [];
  for (const { kind, id, version, api_version, bundle } of spec.entries) {
    const payload = await readFile(resolve(dirname(specFile), bundle));
    entries.push({
      kind,
      id,
      version,
      api_version,
      bytes: payload.length,
      sha256: createHash('sha256').update(payload).digest('hex'),
    });
  }
  await writeJSON(output, { schema: 1, expires_at: expiresAt, entries });
} else if (command === 'sign' && args.length === 4 && keyID.test(args[2])) {
  const [index, privatePEM, id, output] = args;
  const key = createPrivateKey(await readFile(privatePEM));
  if (key.asymmetricKeyType !== 'ed25519') {
    throw new Error('Catalog signing requires an Ed25519 key');
  }
  const signature = sign(null, await readFile(index), key).toString('base64');
  await writeJSON(output, { key_id: id, signature });
} else if (command === 'keyring' && args.length === 3 && keyID.test(args[1])) {
  const [publicPEM, id, output] = args;
  const key = createPublicKey(await readFile(publicPEM));
  if (key.asymmetricKeyType !== 'ed25519') {
    throw new Error('Catalog trust requires an Ed25519 public key');
  }
  const { x } = key.export({ format: 'jwk' });
  await writeJSON(output, {
    keys: { [id]: Buffer.from(x, 'base64url').toString('base64') },
    revoked: [],
  });
} else {
  throw new Error(
    'Usage: catalog.mjs index SPEC OUTPUT EXPIRES_AT | sign INDEX PRIVATE_PEM KEY_ID OUTPUT | keyring PUBLIC_PEM KEY_ID OUTPUT',
  );
}
