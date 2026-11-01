# Quizzes, gates and browser progress

[العربية](learning.ar.md) · [API index](README.md) · [Owner tools](../owner-tools.md)

## Immutable authoring

Owner `PUT /api/v1/content/{key}/{locale}` accepts optional `quiz`:
`assessment_id`, `pass_mark`, `show_explanations`, and `questions` with
`id`, `type` (`single`, `multi`, `bool`), `text`, `options:[{id,text}]`,
`correct:[optionID]` and `explanation`. Omission removes it from the new draft only.
Docs reject quizzes. Correct answers are private authoring fields.

There are 1–20 questions, 2–8 options, stable unique IDs matching
`[a-zA-Z0-9_-]{1,64}`, one correct single/bool answer and one or more multi answers.
Bool options are `true`, `false` in that order. Prompt/option/explanation limits
are 2000/1000/4000 code points; canonical/localized JSON limits are 64/256 KiB.
The integer pass mark is 1–question count. Each question earns one point;
multi-select requires exact set equality. Reject missing, duplicate or unknown
answers. Unlimited retries are the learning policy; rate/storage bounds are separate.

Immutable assessments hold canonical IDs, answers, pass mark and feedback policy.
A localized binding belongs to an exact content revision. Translations must match
the current source assessment and have exact owner review before publication.
Outdated translated bodies can remain live, but their quizzes cannot grant new
passes; the reader gets available-language links. Draft edits do not change live
bindings. Restore creates a new revision that needs new translation review.

All owner paths below use `/api/v1`, the owner cookie and Origin/CSRF for writes:

| Endpoint | Contract |
| --- | --- |
| `POST /content/{key}/{locale}/quiz-preview` | Saved draft `{version:{quiz,content},answers,idempotencyKey}`; pure server grading, no identities/attempts/completion. |
| `POST /content/{key}/{locale}/quiz-review` | `{expected_version,revision_id}` records exact assessment review. |
| `GET /content/{key}/{locale}/quiz-gate` | Draft context `{key,version,plan_id,enabled}`. |
| `PUT /content/{key}/{locale}/quiz-gate` | `{expected_version,plan_id,assessment_id,enabled}` clones a reviewed draft boundary. The final boundary also gates future appended lessons. |
| `POST /collections/{key}/apply` | Exact saved plan/version; validates reviewed published source assessments and safe order. Retained unavailable prerequisites stay closed. |
| `POST /content/{key}/{locale}/publish` | Optional `plan:{expected_version,plan_id}` publishes a changed gated source and applies the matching reviewed plan atomically. A stale plan rolls back publication. |
| `POST /collections/{key}/progress-reset` | `{expected_version}`; fresh full owner authentication, rechecked in the transaction. Increments epoch and removes obsolete completion. Never implicit in edits. |

All preceding gates count, not merely the preceding lesson. Stable source lesson
IDs and the active path epoch determine earned passes. Ordinary edits/reorders
preserve them. A target cannot cross a retained prerequisite during reorder; remove
that gate explicitly first if changing policy is intentional. Active prerequisites
cannot silently lose or replace their assessment. Unpublication does not unlock
anything; existing passes still count. The owner sees the affected path before
publication/plan changes and a separate reset confirmation.

## Reader APIs and privacy

- Published locked content returns 403 `lesson_locked` with safe title/summary and
  prerequisite links, never Markdown, media references or quiz questions. Locked
  HTML is 403/noindex. Unpublished content is 404; Soon contains only explicit
  announcements. Media GET/HEAD/Range use the same live decision, including posters
  and caption references. Search exposes summaries only; protected bodies are not
  prefetched. Reuse in another accessible reference makes that media accessible.
- `GET /api/v1/learning/content/{key}/{locale}/quiz` returns `ReaderLearning`:
  `{version,quiz?,completed,can_mark_read,unavailable,alternatives,prerequisites}`.
  `quiz` contains questions but no answers/explanations. Public content includes the
  same projection. Version contains `quiz`, `content`, and lesson `path`/`epoch`.
- `POST .../attempts` accepts `{version,answers:{questionID:[optionID]},idempotencyKey}`.
  Validate identity, current access and exact versions; record attempt, earned pass
  and replay result atomically. Changed-key payload or stale version → 409. Response
  includes score/total/pass, feedback, completion and an accessible `unlockedHref`
  where available. Only enabled feedback reveals correct IDs/explanations after
  submission. No client `passed`/`unlocked` field is accepted.
- `POST .../read` accepts `{version}` for an open non-quiz lesson only. It cannot
  bypass a quiz or a prerequisite. Collection outlines report completion separately
  from publication counts. Completion persists across refresh and locale changes.

## Browser identity, limits and retention

`POST /learning/session` requires exact Origin and `X-Blog-Request: 1`. It reuses a
valid browser identity or creates 32 random bytes in a `__Host-learning` Secure,
HttpOnly, SameSite=Lax cookie. Only its SHA-256 hash/internal ID/timestamps are
stored. The response provides the learning CSRF token. Owner cookies remain separate.
No identity is created by a GET or owner preview. Public Save my progress and quiz
submission are explicit interactions. Progress is saved in this browser, without
account recovery or cross-device sync.

`GET /learning/progress` returns only the current cookie's state and completed
stable item/collection keys, epoch and method. Without a cookie it returns inactive
and creates nothing. Existing activity renews idle expiry. No caller-supplied
identity is accepted. `DELETE /learning/progress` requires exact Origin and learning
CSRF; it deletes the identity, attempts and achievements and expires the cookie.
The UI reloads immediately so reset cannot leave an unlocked page displayed.
All learning writes require the same Origin/CSRF checks. Responses are no-store.

Defaults: `BLOG_LEARNING_IDLE_DAYS=180` (1–3650),
`BLOG_LEARNING_MAX_IDENTITIES=10000` (1–1000000). Creation is limited to 10/minute
per trusted client IP. Attempts allow 10/minute per identity in SQLite plus
100/minute per IP; bounded in-memory IP hashes expire after one minute. 429 is a
temporary abuse limit, not a lifetime retry limit. Retain at most 100 recent attempt
results per identity for at most 30 days. Exact replay is guaranteed within that
retention window; after pruning, a still-current submission can be a new attempt.
No raw selected answers are retained. Durable completion is separate and survives
attempt pruning. Identity expiry denies access immediately and deletion cascades
all progress. Path reset removes prior-epoch completion.

Hourly batched cleanup runs automatically. `build/blog cleanup-learning` performs a
complete sweep with the operator environment. No credentials or learner data are
seeded. Tests cover concurrent replay, stale versions, translation changes, all
earlier boundaries, unpublication, safe reorder, epoch reset and EN/AR/FR real-server
browser progression, private media denial and reset. Virtual owner authenticators
do not prove physical passkey compatibility; verify this on the target devices.

Enabled feedback must also serialize to at most 120000 bytes, leaving room in the 128 KiB retained result.
