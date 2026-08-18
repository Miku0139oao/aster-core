# Documentation deploy

## Local build

`npm run build` in `docs/` runs:

1. `scripts/prepare-assets.mjs`
   - Copies `logo.png` and `config.yaml` into `.vitepress/public-runtime/` as the VitePress `publicDir`.

2. `vitepress build .`
   - Builds `docs/*.md` and `docs/**/*.md`. New pages are generated automatically from the matching Markdown files.
   - `cleanUrls: true` produces extensionless paths such as `downloads.html`, `changelog.html`, and `en/index.html`.
   - The `sitemap` setting adds every built page to `sitemap.xml`.

3. `scripts/prepare-sites-dist.mjs`
   - Outputs `dist/client/` (the static site).
   - The production host is Caddy with `try_files` fallback, so `_worker.js` and `dist/server/` are not produced.

## Hosting

- Host: `root@miku.zerotwo02.net` over SSH.
- Document root: `/srv/astercore-docs/current`, a `ln -sfn` to `releases/<date>-<sha8>`.
- Web server: Caddy with the clean URL `try_files` fallback.
- Front: Cloudflare is set to DYNAMIC; it only fronts and resolves DNS and does not serve static routes or Functions.

Example Caddy site:

```caddy
astercore.fubukishop.app {
    root * /srv/astercore-docs/current
    file_server
    try_files {path} {path}.html {path}/index.html
}
```

## Deploy commands

```sh
cd docs
npm run build
node scripts/deploy-site.mjs
```

Preview with a dry run:

```sh
node scripts/deploy-site.mjs --dry-run
```

Manual tar+SSH example (the script avoids PowerShell `$( )` expansion issues):

```sh
dt=$(date +%Y%m%d)
sha=$(git rev-parse --short=8 HEAD)
name=${dt}-${sha}
rel=/srv/astercore-docs/releases/${name}

tar -czf - -C dist/client . | ssh root@miku.zerotwo02.net \
  "mkdir -p ${rel} && tar -xzf - -C ${rel} && cd /srv/astercore-docs && ln -sfn ${name} current"
```

## How new routes work

- `/downloads`: create `docs/downloads.md`, VitePress builds `downloads.html`; Caddy `try_files` serves it as `{path}.html`.
- `/changelog`: create `docs/changelog.md`, VitePress builds `changelog.html`.
- `/en/`: create `docs/en/index.md`, VitePress builds `en/index.html`.

`try_files {path} {path}.html {path}/index.html` handles `/downloads`, `/changelog`, `/en/`, `/en`, and unknown paths return 404.

The full Traditional Chinese write-up is at [文件發布流程](/development/docs-deploy).
