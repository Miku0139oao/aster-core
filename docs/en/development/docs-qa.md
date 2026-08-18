# Documentation site QA

This page records how to verify the Aster Core VitePress site.

```sh
cd docs
npm ci
npm run build
npm run check:docs
npm run check:docs:required
```

`check:docs` crawls built HTML, sidebar targets, sitemap, local search, and Worker clean URLs. `check:docs:required` also requires `/en/`, `/downloads`, and `/changelog`.

The detailed 2026-08-19 matrix is in the [Traditional Chinese QA record](/development/docs-qa).
