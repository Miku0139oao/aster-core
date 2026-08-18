# 文件發布流程

`npm run build` 會依序執行：

1. `scripts/prepare-assets.mjs`
   - 把 `logo.png` 與 `config.yaml` 複製到 `.vitepress/public-runtime/`，作為 VitePress `publicDir`。

2. `vitepress build .`
   - 編譯 `docs/*.md` 與 `docs/**/*.md`；新增頁面只要建立對應的 Markdown 檔案就會被自動產生。
   - `cleanUrls: true` 會產生 `downloads.html` / `changelog.html` / `en/index.html` 等無副檔名的路徑。
   - `sitemap` 設定會自動把新頁面加入 `sitemap.xml`。

3. `scripts/prepare-sites-dist.mjs`
   - 輸出 `dist/client/`（靜態網站）與 `dist/server/index.js`（Worker 備份）。
   - 同時把 `worker/index.js` 複製為 `dist/client/_worker.js`，讓 Cloudflare Pages 以 Functions 方式提供 SPA / clean URL fallback。

## 新增路由如何生效

- `/downloads`：建立 `docs/downloads.md`，VitePress 產生 `downloads.html`。
- `/changelog`：建立 `docs/changelog.md`，VitePress 產生 `changelog.html`。
- `/en/`：建立 `docs/en/index.md`，VitePress 產生 `en/index.html`。

`_worker.js` 會在 Cloudflare Pages 回傳 404 時，依序嘗試 `{path}.html` 與 `{path}/index.html`（含帶斜線的版本），因此造訪 `/downloads`、`/changelog`、`/en/`、`/en` 都不會出現 `.html` 404。

部署時只要上傳 `dist/client` 的內容（含 `_worker.js` 與 `sitemap.xml`）即可。
