# 0002. Biome lints and formats the UI

- Status: Accepted
- Date: 2026-11-25
- Links: #7

## Context

The UI had no linter or formatter, so style was kept consistent by hand. ESLint with typescript-eslint, plus Prettier, is the usual setup. typescript-eslint supports TypeScript up to 6.0, and the UI uses TypeScript 7.

Biome lints and formats in one tool and parses TypeScript itself, so it doesn't depend on the `typescript` package. Its React rules cover what eslint-plugin-react-hooks would: hooks called at the top level, and effect dependencies.

## Decision

Biome, configured in `web/biome.json`, lints and formats `web/src`. It runs as `npm run lint` in CI. Formatting uses single quotes, no semicolons, and a 120-character line width. That width was closest to the existing code, so the first reformat was small.

`style/noNonNullAssertion` is off, because the code uses `!` on values a query's `enabled` flag already guards. Any other exception is a `biome-ignore` comment that says why.

## Consequences

- One dev dependency replaces several, and nothing ties the linter to the TypeScript version.
- ESLint plugins can't be used. Rules that only exist as ESLint plugins, such as those for Testing Library or accessibility beyond what Biome covers, aren't available.
- Moving to ESLint once typescript-eslint supports TypeScript 7 would mean a new record and a second reformat.
