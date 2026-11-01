import assert from 'node:assert/strict';
import { createHash, randomBytes } from 'node:crypto';

// Runs only against the smoke suite's isolated instance and virtual owner.
export async function smokeOAuth({ context, page, origin, locale, t }) {
  const state = await (await context.request.get(origin + '/api/v1/session')).json();
  const headers = { Origin: origin, 'X-CSRF-Token': state.csrf };
  const parentResponse = await context.request.post(origin + '/api/v1/security/publisher-tokens', {
    headers,
    data: {
      label: 'OAuth browser check',
      scopes: ['draft'],
      collections: [],
      allow_articles: true,
      days: 1,
    },
  });
  assert.equal(parentResponse.status(), 201);
  const parent = await parentResponse.json();
  const callback = 'https://chatgpt.com/connector_platform_oauth_redirect';
  const registration = await context.request.post(origin + '/oauth/register', {
    data: { redirect_uris: [callback], token_endpoint_auth_method: 'none' },
  });
  assert.equal(registration.status(), 201);
  const client = await registration.json();
  const verifier = randomBytes(32).toString('base64url');
  const clientState = randomBytes(16).toString('hex');
  const resource = origin + '/api/v1/publisher/mcp';
  const query = new URLSearchParams({
    client_id: client.client_id,
    redirect_uri: callback,
    response_type: 'code',
    resource,
    scope: 'publisher offline_access',
    state: clientState,
    code_challenge: createHash('sha256').update(verifier).digest('base64url'),
    code_challenge_method: 'S256',
  });
  const authorization = await context.request.get(origin + '/oauth/authorize?' + query, {
    maxRedirects: 0,
  });
  assert.equal(authorization.status(), 302);
  const consentURL = new URL(authorization.headers().location, origin);
  consentURL.pathname = `/${locale}/admin/oauth`;
  await page.route(callback + '**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'text/html',
      body: '<p>Callback intercepted by local test.</p>',
    }),
  );
  await page.goto(consentURL.href);
  await page.getByRole('heading', { name: t('oauth.title'), exact: true }).waitFor();
  await page.getByLabel(t('oauth.permission'), { exact: true }).selectOption(parent.id);
  await page.getByRole('button', { name: t('oauth.allow'), exact: true }).click();
  await page.waitForURL((url) => url.origin === 'https://chatgpt.com');
  const returned = new URL(page.url());
  assert.ok(returned.searchParams.get('state') === clientState);
  assert.ok(returned.searchParams.get('iss') === origin);
  const exchange = await context.request.post(origin + '/oauth/token', {
    form: {
      grant_type: 'authorization_code',
      client_id: client.client_id,
      redirect_uri: callback,
      code: returned.searchParams.get('code'),
      code_verifier: verifier,
      resource,
    },
  });
  assert.equal(exchange.status(), 200);
  const credential = await exchange.json();
  const key = 'oauth-browser-' + locale;
  const response = await context.request.post(resource, {
    headers: {
      Authorization: 'Bearer ' + credential.access_token,
      Accept: 'application/json, text/event-stream',
      'MCP-Protocol-Version': '2025-11-25',
    },
    data: {
      jsonrpc: '2.0',
      id: 1,
      method: 'tools/call',
      params: {
        name: 'save_draft',
        arguments: {
          key,
          locale: 'en',
          idempotency_key: key,
          draft: {
            expected_version: 0,
            kind: 'article',
            source_locale: 'en',
            title: 'OAuth private browser check',
            slug: key,
            markdown: 'Private draft.',
          },
        },
      },
    },
  });
  assert.equal(response.status(), 200);
  const envelope = await response.json();
  assert.equal(JSON.parse(envelope.result.content[0].text).status, 200);
  await page.goto(origin + `/${locale}/admin/security`);
  await page.getByRole('heading', { name: t('oauth.connections'), exact: true }).waitFor();
  await page.getByRole('button', { name: t('oauth.disconnect'), exact: true }).click();
  await page
    .getByRole('button', { name: t('oauth.disconnect'), exact: true })
    .waitFor({ state: 'detached' });
  const denied = await context.request.post(resource, {
    headers: { Authorization: 'Bearer ' + credential.access_token },
    data: {},
  });
  assert.equal(denied.status(), 401);
  // Disconnect must leave the parent credential usable. Revoke this fixture
  // explicitly only after checking that boundary, so later stages stay isolated.
  const parentCheck = await context.request.post(resource, {
    headers: {
      Authorization: 'Bearer ' + parent.secret,
      Accept: 'application/json, text/event-stream',
      'MCP-Protocol-Version': '2025-11-25',
    },
    data: { jsonrpc: '2.0', id: 2, method: 'tools/list', params: {} },
  });
  assert.equal(parentCheck.status(), 200);
  const currentSession = await (await context.request.get(origin + '/api/v1/session')).json();
  const cleanup = await context.request.delete(origin + '/api/v1/security/publisher-tokens', {
    headers: { Origin: origin, 'X-CSRF-Token': currentSession.csrf },
    data: { id: parent.id },
  });
  assert.equal(cleanup.status(), 200);
  await page.unroute(callback + '**');
}
