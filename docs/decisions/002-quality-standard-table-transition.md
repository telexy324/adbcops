# ADR 002: Separate legacy and structured quality standards

## Status

Accepted for Knowledge Center 2.0 Task 1.7A.

## Context

The v1.3 design assigns `kb_quality_standard` to the versioned Standard/Profile/Criterion/Rule model. The existing application already used that table name for uploaded Word/Excel/PDF reference files with incompatible non-null columns. Reusing the old shape would weaken the 2.0 constraints, while dropping it would break the existing document quality API and stored references.

## Decision

Migration 000036 renames the existing table to `kb_quality_standard_legacy` and updates the legacy GORM mapping. It then creates the v1.3 `kb_quality_standard` aggregate and child tables under their specified names. The old `/api/documents/quality-standards` endpoints continue to use the legacy table; new `/api/knowledge/quality-standards` endpoints exclusively use the structured model.

The migration seeds one published built-in standard. Application-created standards always start as drafts, and published aggregates are immutable.

## Consequences

- Existing uploaded standard files and APIs remain usable.
- New code can follow the v1.3 schema without nullable compatibility columns.
- Consumers must select the API namespace matching the legacy file or structured standard model.
- The later import task can retain source files in the legacy/document version storage and link the generated structured draft through `source_document_version_id`.
