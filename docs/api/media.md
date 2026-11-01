# Controlled media API

[العربية](media.ar.md) · [API index](README.md)

Owner uploads use controlled delivery. No runtime data is
seeded. All browser mutations require a full owner session, exact Origin and
`X-CSRF-Token`; scoped publisher support belongs to 034.

- `POST /api/v1/media?purpose=content`: multipart with exactly one `file` part.
  Returns 201 and `{id,url,kind,mime_type,byte_size,filename,purpose,width,height,created_at}`.
  The URL is `/media/{id}`; paths and checksums stay internal. Purposes are
  `content`, `logo`, `favicon`, `portrait`, `banner`.
- `GET /api/v1/media?kind=image&cursor=…`: private list, up to 50 records and
  `next_cursor`; filtering is optional. IDs are opaque generated lowercase hex.
- `DELETE /api/v1/media/{id}`: owner-only, rejects retained revision/configuration
  references with 409. Replacement means a new upload, never overwriting bytes.
- `GET/HEAD /media/{id}`: authorizes before opening bytes and before processing
  Range. Unreferenced/draft-only media returns 404 to the public. Applied public
  configuration or at least one currently accessible published revision permits
  access. Missing lesson-gate policy fails closed. Later learning gates use this
  same policy; no static upload directory exists.

JPEG/PNG/WebP are fully decoded after checking dimensions (at most 24 million
pixels, each axis at most 8192) and limited to 10 MiB. Logo/favicon: 1 MiB;
portrait: 2 MiB, square, at least 600×600; banner: 10 MiB. Configuration must
recheck purpose limits when reusing an existing media ID.

Video: at most 50 MiB, one video track at most 4096×4096, optional one audio
track. MP4 allows H.264 (`avc1` with SPS/PPS) and optional AAC-LC. WebM allows
VP8/VP9 and optional Opus/Vorbis. Parsers validate container metadata and declared
codec combinations; they do not transcode or guarantee arbitrary damaged frames
will decode. Browser fixtures verify actual playback and seeking separately.
PDF: 20 MiB, header/cross-reference/EOF validation, always attachment with sandbox
CSP and nosniff; this is not malware scanning. UTF-8 WebVTT: 1 MiB, bounded valid
cue timestamps. SVG, executable bytes and declared-type mismatches are rejected.

Uploads use generated private files, fsync and same-filesystem rename, then
metadata, under the revision/maintenance lock. Interrupted writes never create a
live metadata reference. Unlink follows metadata deletion; failure leaves an
orphan for explicit maintenance. File checksums are verified before serving.
Responses use `no-store`; a single bounded byte range supports seeking. Multi-range
requests return 416. Already downloaded bytes cannot be revoked.

Validation: `go -C backend test ./internal/media ./internal/storage ./internal/http ./internal/db`.
`scripts/smoke-auth.mjs` invokes `smoke-media.mjs` against the real HTTPS owner
session and generated blue 32×32 MP4/WebM fixtures. Fixtures contain no user data.
Run after build/frontend tests and Playwright Chromium installation. Browser
tests do not establish Safari/Firefox/device compatibility; target-device acceptance
remains part of 039.
