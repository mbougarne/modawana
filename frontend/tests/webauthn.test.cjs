const { test } = require('node:test');
const assert = require('node:assert/strict');
const {
  creationOptions,
  requestOptions,
  credentialJSON,
} = require('../.test-output/frontend/src/features/auth/webauthn');
test('WebAuthn JSON conversion preserves UV, discoverability and native authenticator choices', () => {
  const options = creationOptions({
    challenge: 'AQID',
    rp: { id: 'blog.test', name: 'Owner' },
    user: { id: 'BAUG', name: 'Owner', displayName: 'Owner' },
    pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
    authenticatorSelection: { residentKey: 'required', userVerification: 'required' },
    excludeCredentials: [{ type: 'public-key', id: 'BwgJ', transports: ['usb', 'hybrid'] }],
  });
  assert.deepEqual([...new Uint8Array(options.challenge)], [1, 2, 3]);
  assert.deepEqual([...new Uint8Array(options.user.id)], [4, 5, 6]);
  assert.equal(options.authenticatorSelection.userVerification, 'required');
  assert.equal(options.authenticatorSelection.authenticatorAttachment, undefined);
  assert.deepEqual(options.excludeCredentials[0].transports, ['usb', 'hybrid']);
  const login = requestOptions({ challenge: 'AQID', userVerification: 'required' });
  assert.equal(login.allowCredentials, undefined);
  assert.equal(login.userVerification, 'required');
  assert.throws(() => requestOptions({ challenge: '%%%' }));
});
test('WebAuthn response serialization preserves signed binary data and user handle', () => {
  global.AuthenticatorAttestationResponse = class {};
  const buf = new Uint8Array([251, 255, 0]).buffer;
  const r = credentialJSON({
    id: '-_8A',
    rawId: buf,
    type: 'public-key',
    authenticatorAttachment: 'cross-platform',
    response: { clientDataJSON: buf, authenticatorData: buf, signature: buf, userHandle: buf },
    getClientExtensionResults: () => ({}),
  });
  assert.equal(r.response.userHandle, '-_8A');
  assert.equal(r.response.signature, '-_8A');
  assert.equal(r.rawId, '-_8A');
  assert.equal(r.authenticatorAttachment, 'cross-platform');
  assert.deepEqual(r.clientExtensionResults, {});
});

test('Transfer reauthentication retries once with the renewed CSRF token and the same ZIP', async () => {
  const {
    authenticatedTransfer,
  } = require('../.test-output/frontend/src/features/auth/reauthenticate');
  const oldFetch = global.fetch;
  const prior = Object.getOwnPropertyDescriptor(global, 'navigator');
  const calls = [];
  let attempts = 0;
  const bytes = new Uint8Array([1, 2, 3]).buffer;
  global.AuthenticatorAttestationResponse = class {};
  Object.defineProperty(global, 'navigator', {
    configurable: true,
    value: {
      credentials: {
        get: async () => ({
          id: 'AQID',
          rawId: bytes,
          type: 'public-key',
          response: {
            clientDataJSON: bytes,
            authenticatorData: bytes,
            signature: bytes,
            userHandle: bytes,
          },
          getClientExtensionResults: () => ({}),
        }),
      },
    },
  });
  global.fetch = async (url, options) => {
    calls.push({ url, options });
    if (url.includes('/portability/')) {
      return { status: ++attempts === 1 ? 403 : 200, ok: attempts > 1 };
    }
    const json = url.endsWith('/session')
      ? { state: 'owner', csrf: 'old' }
      : url.endsWith('/start')
        ? { csrf: 'challenge' }
        : url.endsWith('/options')
          ? { publicKey: { challenge: 'AQID', userVerification: 'required' } }
          : { state: 'owner', csrf: 'renewed' };
    return { ok: true, json: async () => json };
  };
  try {
    const zip = new Blob(['zip']);
    const result = await authenticatedTransfer('/api/v1/portability/notion/import', {
      method: 'POST',
      body: zip,
    });
    assert.equal(result.status, 200);
    const uploads = calls.filter((entry) => entry.url.includes('/portability/'));
    assert.equal(uploads.length, 2);
    assert.equal(uploads[1].options.body, zip);
    assert.equal(uploads[1].options.headers['X-CSRF-Token'], 'renewed');
  } finally {
    global.fetch = oldFetch;
    if (prior) {
      Object.defineProperty(global, 'navigator', prior);
    } else {
      delete global.navigator;
    }
  }
});
