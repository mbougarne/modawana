# Immutable revision storage

[العربية](revision-storage.ar.md)

The application exposes the
[content API](api/content.md), with a 1 MiB Markdown limit and idempotent mutations. `content.Revisions.Save` writes complete UTF-8 Markdown
(up to 2 MiB), including a fresh copy for metadata-only edits, then compares the
expected version and commits SQLite metadata/draft pointers. Published pointers
remain unchanged. Expected version zero creates the first language version of an
existing item; subsequent edits require the current positive version.

Files use generated `content/<item-id>/<en|ar|fr>/<revision-id>.md` keys. A private
same-directory temporary file is flushed, renamed, and its directory flushed
before SQLite can reference it. New parent entries are flushed too. SHA-256 and
byte length are checked on every read; missing or corrupt bodies fail explicitly,
never becoming an empty article. An intentionally saved empty body is distinct.
Go's directory-root APIs confine filesystem access; path grammar and symlink
checks reject caller paths and links. Physical paths are never API fields.

All saves hold the cross-process `maintenance.lock` through metadata commit.
Backups and future collectors must acquire the same exclusive lock. A snapshot
must retain **all** referenced revision history, not just current/live files.
`InspectLocked` compares references with files and reports invalid references and
orphans without deleting anything. Failed saves, conflicts and interrupted renames
may leave unreferenced files; no automatic garbage collection is enabled. Do not
manually delete files while the application or backup is running. Use the coordinated commands in [recovery](operations/recovery.md).

A failure after commit can mean the save succeeded despite a missing response;
direct callers must reload current metadata before retrying. The content API records idempotency results atomically; callers can replay the same key and payload. Fault-injection tests cover before/after rename, directory sync and
SQLite commit, preserved readable pointers, conflicts, metadata-only copies,
traversal/symlink rejection, corruption/missing files and maintenance exclusion.

Test with `go -C backend test ./internal/storage ./internal/content`. The file protocol requires a local filesystem that supports durable
file and directory sync; failure to sync aborts the save.
