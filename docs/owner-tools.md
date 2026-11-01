# Writing and managing content

[العربية](owner-tools.ar.md) · [Documentation](README.md)

Sign in at `/{locale}/login` and use the owner dashboard. The interface supports
English, Arabic and French. All owner routes require a full session; browser writes
also require the same origin and CSRF protection.

## Write and publish

1. Open **Content** and create an Article, Lesson or Doc. Give it a title and choose
   the content language. New titles suggest slugs; you can edit them before publication.
   Existing published slugs stay fixed, even after unpublishing.
2. Use **Write**, **Preview**, **Media**, **Quiz** and **Details** to edit the same
   working draft. Docs do not have quizzes. The split preview uses the public
   Markdown renderer. Long content scrolls inside the editor.
3. Choose, paste or drop media, or select an existing library file. Add alternative
   text, insert its stable `/media/{id}` reference, and optionally choose a cover.
   Videos can use an uploaded image poster, VTT captions and a plain-text transcript.
4. **Save draft** creates an immutable revision. **Save and publish** first saves
   the current work, then publishes that exact revision/version. A cancelled or failed
   save never publishes an older revision. You can start another item with **New content**.

Publish/unpublish and history restoration check versions. Restoring history creates
a new draft and leaves the live revision unchanged. After a conflict, export the
unsaved draft before reloading; the editor never silently overwrites newer work.
Retrying an unchanged save reuses the same operation key.

Translations record their source revision and require review before publishing.
For quiz translations, **Use current source assessment** refreshes canonical quiz
structure while retaining text for matching IDs; review the whole translation again.

The content list filters language, type, publication status and text with pagination.
Bulk publication shows a confirmation and uses exact selected versions. A stale,
inaccessible or unreviewed selection is rejected by the server.

## Collections, categories and tags

Create or open a collection, set its localized details, search by content title,
select a result and click **Add**. Repeat, reorder, then **Save collection**.
Selected articles become lessons or docs for the collection kind after confirmation.
Published content stays published; drafts stay private. Save commits details, order,
plan and activation together. Advanced controls expose separate plan/version/progress
operations. Published members cannot silently move or disappear.

For a lesson, choose a learning collection in Details and save. **Publish with path
plan** can publish the lesson and activate its matching plan atomically. If a plan is
missing, the workflow prepares a private ordered plan after confirmation. Publication
alone does not activate a plan. Enable both the collection's localized visibility
and **Profile → Show learning paths** to show it on the home page.

A roadmap entry may explicitly announce a private lesson as “Soon”; leaving its
announcement empty keeps it private. A teaser never grants access to the body.

Categories and tags have stable keys and localized labels/slugs. Create them from
the taxonomy screen or the editor's pickers. Fixed keys have no generator control.
Changing an item's type uses the owner editor's saved conversion and confirmation;
server publication, collection and quiz constraints still apply.

## Quizzes and lesson gates

Article/lesson quizzes support 1–20 single-choice, multiple-choice or true/false
questions, 2–8 options, one point per question, an integer pass mark and unlimited
retries. Stable question/option IDs preserve meaning through reordering and translation.
The quiz is bound to the exact content revision; editing a draft does not replace a live quiz.

**Save and try quiz** grades the saved quiz privately on Go without recording learner
progress. **Save and review assessment** confirms that exact assessment. To gate later
lessons, order at least two lessons, review the earlier quiz, enable its draft unlock
rule and publish with the matching path plan after confirming the impact. A quiz
lesson does not lock itself. Article quizzes are practice and cannot create path gates.
Resetting a path for all readers is a separate fresh-authentication operation.

Reader identities are created only on explicit interaction, not page visits.
Progress is scoped to the browser and can expire or be reset. Correct answers and
explanations appear only after server grading when feedback is enabled.

## Media and site settings

The media library filters images, videos, PDFs and VTT captions. A file referenced
by content, configuration or retained history cannot be deleted. If another public
reference already exposes a file, attaching it to a gated lesson does not make the
same URL private. Review the public-reuse warning before sharing protected material.

Use **Settings** for colours and logo, **Profile** for introduction, portrait, Now,
experience and links, and **Labels** for interface text. Preview/Cancel does not change
the public site; save and apply the reviewed version. The profile's **Main action**
controls the primary Home/About button; choosing an icon only changes its symbol.
Visible configured GitHub, LinkedIn and RSS links also populate the footer. Footer
contact text can replace the default email-linked call to action.

Use **Transfer** for [draft-only import/export](api/portability.md), and **Security**
for [passkeys/recovery](owner-security.md), publisher tokens and connected publishing apps.
Technical details: [content API](api/content.md), [learning](api/learning.md),
[configuration](api/configuration.md), and [verification](../CONTRIBUTING.md).
