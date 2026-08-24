# 文件發布流程

## 本機建置

`npm run build` 會依序執行：

1. `scripts/prepare-assets.mjs`
   - 把 `logo.png` 與 `config.yaml` 複製到 `.vitepress/public-runtime/`，作為 VitePress `publicDir`。

2. `vitepress build .`
   - 編譯 `docs/*.md` 與 `docs/**/*.md`；新增頁面只要建立對應的 Markdown 檔案就會被自動產生。
   - `cleanUrls: true` 會產生 `downloads.html` / `changelog.html` / `en/index.html` 等無副檔名的路徑。
   - `sitemap` 設定會自動把新頁面加入 `sitemap.xml`。

3. `scripts/prepare-sites-dist.mjs`
   - 輸出 `dist/client/`（靜態網站）。
   - 正式主機由 Caddy 以 `try_files` 提供 clean URL fallback，因此不需要輸出 `_worker.js` 或 `dist/server/`。

## 實際託管方式

- 主機：`root@miku.zerotwo02.net`（SSH）。
- 靜態根目錄：`/srv/astercore-docs/current`（`ln -sfn` 指向上方 `releases/<date>-<sha8>`）。
- Web server：Caddy，以 `try_files` 提供 clean URL fallback。
- 前置：Cloudflare 設定為 DYNAMIC，僅做前置與 DNS，不負責靜態路由或 Functions。

Caddy 站點設定範例：

```caddy
astercore.fubukishop.app {
    root * /srv/astercore-docs/current
    file_server
    try_files {path} {path}.html {path}/index.html
}
```

## 部署命令

在 `docs/` 執行：

```sh
npm run build
node scripts/deploy-site.mjs
```

或以 `--dry-run` 預覽：

```sh
node scripts/deploy-site.mjs --dry-run
```

手動 tar+SSH 範例（腳本已避免 PowerShell `$( )` 展開問題）：

```sh
dt=$(date +%Y%m%d)
sha=$(git rev-parse --short=8 HEAD)
name=${dt}-${sha}
rel=/srv/astercore-docs/releases/${name}

tar -czf - -C dist/client . | ssh root@miku.zerotwo02.net \
  "mkdir -p ${rel} && tar -xzf - -C ${rel} && ln -sfn ${rel} /srv/astercore-docs/current"
```

## 新增路由如何生效

- `/downloads`：建立 `docs/downloads.md`，VitePress 產生 `downloads.html`；Caddy `try_files` 會自動以 `{path}.html` 回應。
- `/changelog`：建立 `docs/changelog.md`，VitePress 產生 `changelog.html`。
- `/en/`：建立 `docs/en/index.md`，VitePress 產生 `en/index.html`。

Caddy `try_files {path} {path}.html {path}/index.html` 會同時支援 `/downloads`、`/changelog`、`/en/`、`/en` 等路徑；未知路徑仍回 404。