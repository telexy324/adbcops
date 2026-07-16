# ADR 001: Preserve document parse revisions

## Status

Accepted for Knowledge Center 2.0 Task 1.5B.

## Context

The v1.3 design shows a unique constraint on `(document_id, version)`, while the same design requires reparsing to preserve historical results or perform an auditable replacement. The existing platform also stores file fields on `kb_document`, so replacing that table would break current upload, review, search, and citation paths.

## Decision

Keep the existing `kb_document` table during the incremental migration. Add `kb_document_version.revision_no` and enforce uniqueness on `(document_id, version, revision_no)`. The first parse writes revision 1. Reparsing an already attempted version creates the next revision and never overwrites its blocks or parse-quality result.

The original file path and SHA-256 hash are copied to every revision. This preserves traceability while existing APIs continue to use `kb_document` until the later publication/version-management task completes the cutover.

Rows that predate this migration are backfilled with `legacy-unverified:<document_id>` because PostgreSQL cannot hash a server-external upload path during a schema migration. The marker is intentionally distinguishable from SHA-256; all newly uploaded files receive a verified SHA-256 hash.

## Consequences

- Historical parsed ASTs remain queryable and auditable.
- A semantic document version may have multiple parse revisions.
- Callers should cite `document_version_id`, not only the version label.
- The future publication task must select a specific parse revision for publication.
