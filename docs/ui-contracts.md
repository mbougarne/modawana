# Shared UI contracts

[العربية](ui-contracts.ar.md) · [Documentation](README.md)

Shared UI includes self-hosted fonts, generated tokens, ThemeProvider, BrandStyle and I18nProvider. It does not seed an owner or content.

`QuizQuestion` contains only question/option IDs, type and localized text. `QuizForReader` adds locale, pass mark and exact quiz/content/path/epoch versions. Correct options and explanations exist only in `QuizResult.feedback`, returned after server grading. `QuizSubmission` carries versions, answers and an idempotency key; the component reuses that key after an uncertain network failure. The server owns grading, persistence and access enforcement.

Loading, load failure, submission failure, rate limits and stale versions are explicit states. Pending inputs are disabled; duplicate submission is suppressed. Network failure retains choices; retry after a failed grade clears them and uses a fresh key. A changed version or locale remounts the attempt and ignores obsolete responses. A stale result cannot expose a next-action link; reload is required. Only a server-supplied successful local next-action URL is rendered. LockedLesson takes an explicitly public teaser string, never a protected body or arbitrary component.

`pick(value, 'profile')` and `pick(value, 'label')` permit fallback only for those string domains. Published content is a separate locale-specific model; taxonomy labels are already resolved strings. Missing article/doc/lesson/quiz translations require explicit available-language navigation. `resolveLabel()` distinguishes an absent override from an explicitly empty one: empty FR/AR uses effective English, while removing an override restores shipped translations. Count labels use Intl.PluralRules, including Arabic zero/one/two/few/many/other. `label()` and the `Label` component expose actual language/direction when fallback is displayed.

Profile contracts include separate portrait/media identity, localized link labels with stable IDs, visibility and allowlisted icons, a main action, optional banner/alt/focal point, ordered experience with year-month values, About document key, public contact/footer fields and section visibility/counts. Empty Now sections are hidden. No hardcoded profile is imported by the application; test fixtures remain separate from production content.

From the root:

```sh
node scripts/project.mjs check
node scripts/project.mjs build
node frontend/tests/visual.cjs
```

The visual check requires Playwright Chromium (`cd frontend && npx playwright install --with-deps chromium` on a supported CI host). It renders built assets for EN/AR/FR at 320/1280px in light/dark mode, checks language/direction/overflow and captures screenshots for inspection. DOM tests exercise the quiz failure/retry/version behavior and label rules. Neither simulated quiz fixtures nor browser screenshots prove server grading, WebAuthn or hardware compatibility.
