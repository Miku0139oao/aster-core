# Documentation deploy

`npm run build` in `docs/` runs asset prep, VitePress, then `scripts/prepare-sites-dist.mjs`. The worker at `docs/worker/index.js` is copied to `dist/client/_worker.js` for Cloudflare Pages clean URLs.

New pages appear automatically: `docs/downloads.md` → `/downloads`, `docs/changelog.md` → `/changelog`, `docs/en/index.md` → `/en/`.

The full Traditional Chinese write-up is at [文件發布流程](/development/docs-deploy).
