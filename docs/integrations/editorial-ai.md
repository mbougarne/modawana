# Editorial AI

[العربية](editorial-ai.ar.md) · [Documentation](../README.md)

Editorial AI is an optional first-party extension. Open **Editorial AI** in
the owner dashboard to configure the OpenAI connection and a daily request
limit. Collection of editorial context begins when an owner asks for a
suggestion. Generation is disabled initially.

## Connection and quota

The installation must have its existing `BLOG_AUTH_KEY_FILE` configured.
The owner settings screen accepts the provider credential and requires recent
authentication to change it. Modawana encrypts the credential in SQLite.
Settings responses report only whether a credential is saved. Replacing or
removing the credential is available in the same screen. Backups exclude
the credential, and a restored site requires configuration again.

The request limit defaults to 20 per UTC day and accepts values from 1 to
100. Requests reserve one unit immediately before contacting OpenAI.
Timeouts, provider rejections, and unusable responses retain that reservation.
The limit controls request count. OpenAI billing applies separately.

## Choose public context

Open a content item in the editor and expand **Editorial AI**. Choose a
suggestion type and check the exact fields to send to OpenAI. The panel
previews the fields from the current published revision. Modawana rechecks
the revision and anonymous access before contacting the provider.

| Suggestion type | Eligible inputs and draft action |
| --- | --- |
| Summary | Select one or more public text fields. Use the editable result in the draft’s summary. |
| Search title and description | Select public text. Use the two editable results in the draft’s SEO fields. |
| Cover image alt text | Select a public cover image and optionally public text. OpenAI receives the public HTTPS image URL. Use the suggestion in the editor’s media text field or copy it into an image block. |
| Social post text | Select public text. Open the social composer with the suggestion prefilled, then select a destination and preview the post. |
| Translation | In a translation editor, select one title, summary, or excerpt from the public source language. Use the editable result in the translated draft. The current translation review rules still apply. |

Public text fields include the title, summary, and a short plain-text excerpt
from published Markdown. The excerpt excludes link destinations, images,
and code blocks. Page-builder data, assessment answers, unpublished or
unlisted revisions, and gated bodies are excluded. The provider interface
accepts no arbitrary prompt, private upload URL, lead, order, or secret data
as editorial context.

## Review and save

The model can return incomplete or incorrect text. Edit the suggestion,
apply it where appropriate, then use the existing **Save draft**, review,
and publication controls. Generating a suggestion makes no content changes.
The Social Publishing connector retains its separate preview and schedule
steps.

The local installation logs neither prompts nor provider credentials. The
adapter uses the OpenAI Responses API with response storage disabled on the
request. See the [OpenAPI schema](../api/v1.openapi.json) for the owner
settings, preview, and suggestion endpoints.
