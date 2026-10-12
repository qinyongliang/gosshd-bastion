# Repository publication rules

- Keep production deployment notes, infrastructure inventories, personal workspace paths, terminal transcripts, internal learning logs and live-system screenshots in ignored local-only directories. Never add them to Git.
- Public documentation and test fixtures must use reserved example domains and addresses, not real deployment endpoints or machine identities.
- Do not publish operational records, private credentials or environment-specific commands in commits, tags, release notes or CI artifacts.
- Before a push, run `node scripts/check-public-content.mjs` and review the staged diff.
- When repository history has been sanitized, rebase unpublished work onto the cleaned history. Never merge or force-push an old, unsanitized branch.
