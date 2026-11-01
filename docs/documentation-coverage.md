# Documentation coverage

[العربية](documentation-coverage.ar.md) · [Index](README.md)

Project guides use English/Arabic pairs or complete bilingual sections. Third-party
license originals are preserved without translation. The [inventory](documentation-inventory.json)
lists the maintained Markdown files and their language coverage; generated builds,
private notes and dependencies are excluded.

After adding or removing guides, run:

```sh
node scripts/check-docs.mjs --write
node scripts/check-docs.mjs
```

The checker verifies companions, language navigation, local file targets and inventory
freshness. Review translated meaning, commands, limits and contracts separately;
file coverage does not prove semantic equivalence. Product labels also include French.
