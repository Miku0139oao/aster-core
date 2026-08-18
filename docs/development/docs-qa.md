# 文件網站 QA

本頁記錄 Aster Core VitePress 文件站的可重複驗收方式，以及 2026-08-19 對 `docs/site-qa`（基準 `98cb11f8`）執行的結果。因正式網址在此網路可能解析為 fake-IP，本輪以 production build、`vitepress preview` 和產物爬蟲為準。正式站點由 Caddy `try_files {path} {path}.html {path}/index.html` 提供 clean URL fallback；`check:docs` 仍透過 `docs/worker/index.js` 模擬同樣行為。

## 執行方式

```sh
cd docs
npm ci
npm run build
npm run check:docs
npm run preview -- --port 4173
```

`npm run check:docs` 會檢查 build 產物的內部連結與 anchor、sidebar targets、sitemap、local search index，以及 clean URL fallback（以 `docs/worker/index.js` 模擬 Caddy `try_files` 行為）。English 首頁、Downloads 和 Changelog 已經合併到目前 main；一般檢查與 strict gate 都應通過。

這三項 required-content 檢查已具備，CI 應使用 strict gate：

```sh
npm run check:docs:required
```

也可設定 `DOCS_REQUIRE_PLANNED=1` 後執行 `npm run check:docs`，或直接執行 `node scripts/check-docs.mjs --require-planned`。Strict mode 會在下列任一來源、設定或 build route 缺少時回傳非零 exit code：

- English：`docs/en/index.md`、`docs/.vitepress/config.mts` 的 `locales.en`、`/en/`
- Downloads：`docs/downloads.md` 或 `docs/downloads/index.md`、`/downloads`
- Changelog：`docs/changelog.md` 或 `docs/changelog/index.md`、`/changelog`

## 2026-08-19 驗收矩陣

| 範圍 | 狀態 | 驗收內容與證據 |
| --- | --- | --- |
| Production build | PASS（setup 後） | 第一次 `npm run build` 因 fresh worktree 尚未安裝 `vitepress` 而失敗；`npm ci` 後相同命令成功，VitePress 完成 client/server、page render、sitemap 與 Sites dist。另有三則 `caddy` syntax highlighting fallback warning，不影響輸出。 |
| Desktop IA | PASS | 本機 preview、1440×900：首頁主 nav、文件頁完整 sidebar、頁內 outline、上一頁／下一頁均可見；`/guide/getting-started` clean URL 可直接載入；browser console 無 warning/error。 |
| English locale | PASS | `docs/en/index.md`、`docs/.vitepress/config.mts` 的 `locales.en` 與 built `/en/` 均存在。 |
| Downloads | PASS | `docs/downloads.md` 與 built `/downloads` 均存在，頁面提供 rolling release asset 與 checksum 來源。 |
| Changelog | PASS | `docs/changelog.md` 與 built `/changelog` 均存在，並連到 baseline compare 與 `Prerelease-main`。 |
| Local search | PASS | `docs/.vitepress/config.mts` 使用 local provider；build 產生 search index。UI 查詢 `kernel-direct` 能顯示結果，Enter 可開啟 `/deployment/openwrt#kernel-direct-設定原理`。 |
| Built-link crawl | PASS | 爬過所有非 404 HTML 的 `href`／`src`，並核對本機檔案與 HTML anchor；無 404 或 missing anchor。Sidebar 所有設定 link 皆有 build target。 |
| Sitemap | PASS | `docs/.vitepress/dist/sitemap.xml` 包含所有 sidebar route，也涵蓋全部 build page route。 |
| Worker clean URLs | PASS | 正式站點由 Caddy `try_files {path} {path}.html {path}/index.html` 提供；本機 `check:docs` 以 file-backed `ASSETS` stub 對所有 build routes 執行 `docs/worker/index.js`；extensionless 與 directory-index routes、HEAD、HTML cache header 均成功，未知 route 保持 404。 |

## 手動桌面檢查清單

每次 nav、sidebar、locale 或首頁 layout 變更後，在 production preview 重跑：

- 首頁 logo、site title、主 nav 與 hero CTA 都能導向有效 route。
- 文件頁 sidebar 顯示正確 locale 的分組；目前頁、outline、pager 不互相遮擋。
- Locale switch 可由繁中進入 `/en/`，也能返回繁中；兩邊不應導到 404 或錯誤語言的 sidebar。
- Downloads 能按 OS／architecture 找到 release asset，並清楚提供 checksum／版本來源。
- Changelog 的最新版本、日期與 release link 一致，舊版 anchor 可直接分享。
- Search modal 可開啟、輸入、鍵盤選取與關閉；繁中與 English 各能搜尋到各自頁面。
- 直接貼上 extensionless URL、directory URL 與含 anchor URL 都能載入；不存在的 URL 必須是 404。

## 結果判定

Broken internal link、missing sidebar target、sitemap 漏頁、search index 遺失、clean URL fallback regression 或 required-content 缺頁，都是立即阻擋上線的 FAIL。三個 required-content checks 已不再是 report-only；CI 不應降級為只執行一般檢查。
