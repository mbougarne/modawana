import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

// Uses the real owner cookie from smoke-auth; all fixtures are temporary uploads.
export async function smokeMedia({ context, page, origin, root }) {
  const { csrf } = await (await context.request.get(origin + '/api/v1/session')).json();
  for (const [file, mime] of [
    ['clip.mp4', 'video/mp4'],
    ['clip.webm', 'video/webm'],
    ['clip-aac.mp4', 'video/mp4'],
    ['clip-opus.webm', 'video/webm'],
    ['clip-vorbis.webm', 'video/webm'],
  ]) {
    const bytes = await readFile(resolve(root, 'backend/internal/media/testdata', file));
    const response = await context.request.post(origin + '/api/v1/media', {
      headers: { Origin: origin, 'X-CSRF-Token': csrf },
      multipart: { file: { name: file, mimeType: mime, buffer: bytes } },
    });
    assert.equal(response.status(), 201, await response.text());
    const m = await response.json();
    assert.equal(
      (await context.request.get(origin + m.url, { headers: { Range: 'bytes=0-9' } })).status(),
      206,
    );
    // Browser-native playback and seeking against the authenticated media route.
    const result = await page.evaluate(async (url) => {
      const v = document.createElement('video');
      v.muted = true;
      v.controls = true;
      v.preload = 'auto';
      document.body.append(v);
      try {
        await new Promise((ok, no) => {
          v.onloadeddata = ok;
          v.onerror = () => no(Error('video decode failed'));
          v.src = url;
          setTimeout(() => no(Error('video load timeout')), 15000);
        });
        await v.play();
        v.pause();
        await new Promise((ok, no) => {
          v.onseeked = ok;
          v.onerror = () => no(Error('seek failed'));
          v.currentTime = 0.5;
          setTimeout(() => no(Error('seek timeout')), 5000);
        });
        return { width: v.videoWidth, time: v.currentTime };
      } finally {
        v.remove();
      }
    }, m.url);
    assert.equal(result.width, 32);
    assert.ok(result.time >= 0.49);
    const anonymous = await context.browser().newContext({ ignoreHTTPSErrors: true });
    for (const method of ['GET', 'HEAD']) {
      const read = await anonymous.request.fetch(origin + m.url, {
        method,
        headers: { Range: 'bytes=0-9' },
      });
      assert.equal(read.status(), 404);
    }
    await anonymous.close();
    assert.equal(
      (
        await context.request.delete(origin + '/api/v1/media/' + m.id, {
          headers: { Origin: origin, 'X-CSRF-Token': csrf },
        })
      ).status(),
      200,
    );
  }
  console.log('Media: MP4/WebM browser decode, playback, seeking and private range denial passed.');
}
