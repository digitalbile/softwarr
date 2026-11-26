# Decision records

Each record is one decision: what we decided, when, and why. They describe the reasoning at the time, so a record is never rewritten. When a later decision replaces one, mark the old one "Superseded by NNNN".

Read the records that touch your change before proposing something they rule out. If you think a decision is wrong, open an issue that answers its reasoning.

Write a record only for a decision someone would otherwise reopen. Most changes need nothing more than a clear pull request description. The original design is in `docs/superpowers/specs/2026-11-23-softwarr-design.md`.

| # | Decision | Status |
| --- | --- | --- |
| [0001](0001-hand-written-api-types.md) | The frontend API types are written by hand, and a Go test checks their field names | Accepted |
| [0002](0002-biome-for-the-ui.md) | Biome lints and formats the UI | Accepted |
| [0003](0003-handlers-have-no-database.md) | HTTP handlers have no database handle and go through services | Accepted |
| [0004](0004-owner-only-password-sign-in.md) | "Disable local auth" leaves password sign-in to the owner | Accepted |

## Template

```markdown
# NNNN. Title

- Status: Accepted
- Date: YYYY-MM-DD
- Links: #issue, #pr

## Context

What forced a decision, and the evidence.

## Decision

What we do.

## Consequences

What follows, including what we gave up and what this rules out.
```
