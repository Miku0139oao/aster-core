import { readFile, readdir, stat } from "node:fs/promises";
import { dirname, extname, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { resolveConfig } from "vitepress";

import worker from "../worker/index.js";

const docsDirectory = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const outputDirectory = resolve(docsDirectory, ".vitepress", "dist");
const requirePlanned =
  process.argv.includes("--require-planned") ||
  process.env.DOCS_REQUIRE_PLANNED === "1";
const hardFailures = [];
const plannedFailures = [];

function result(status, label, details = "") {
  console.log(`[${status}] ${label}${details ? `: ${details}` : ""}`);
}

function check(label, issues, details) {
  if (issues.length) {
    hardFailures.push(label);
    result("FAIL", label, issues.join("; "));
  } else {
    result("PASS", label, details);
  }
}

async function isFile(path) {
  try {
    return (await stat(path)).isFile();
  } catch {
    return false;
  }
}

async function walk(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = resolve(directory, entry.name);
    files.push(...(entry.isDirectory() ? await walk(path) : [path]));
  }
  return files;
}

function decode(value) {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function exactOutputPath(pathname) {
  const candidate = resolve(
    outputDirectory,
    decode(pathname).replace(/^\/+/, ""),
  );
  return candidate === outputDirectory ||
    candidate.startsWith(`${outputDirectory}${sep}`)
    ? candidate
    : null;
}

async function builtTarget(pathname) {
  const exact = exactOutputPath(pathname);
  if (!exact) return null;
  const candidates = [exact];
  if (pathname.endsWith("/")) {
    candidates.push(resolve(exact, "index.html"));
  } else if (!extname(pathname)) {
    candidates.push(`${exact}.html`, resolve(exact, "index.html"));
  }
  for (const candidate of candidates) {
    if (await isFile(candidate)) return candidate;
  }
  return null;
}

function routeForHtml(path) {
  const name = relative(outputDirectory, path).split(sep).join("/");
  if (name === "404.html") return null;
  if (name === "index.html") return "/";
  if (name.endsWith("/index.html")) {
    return `/${name.slice(0, -"index.html".length)}`;
  }
  return `/${name.slice(0, -".html".length)}`;
}

function collectLinks(value, links = new Set()) {
  if (Array.isArray(value)) {
    for (const item of value) collectLinks(item, links);
  } else if (value && typeof value === "object") {
    if (typeof value.link === "string") links.add(value.link);
    for (const [key, item] of Object.entries(value)) {
      if (key !== "link") collectLinks(item, links);
    }
  }
  return links;
}

function canonical(pathname) {
  const value = decode(pathname);
  return value === "/" ? "/" : value.replace(/\/+$/, "");
}

async function main() {
  if (!(await isFile(resolve(outputDirectory, "index.html")))) {
    result(
      "FAIL",
      "Built output",
      "missing .vitepress/dist/index.html; run npm run build",
    );
    process.exitCode = 1;
    return;
  }

  const config = await resolveConfig(docsDirectory, "build", "production");
  const userConfig = config.userConfig ?? {};
  const themeConfig = userConfig.themeConfig ?? {};
  const origin = new URL(
    config.sitemap?.hostname ?? "https://docs.invalid",
  ).origin;
  const allFiles = await walk(outputDirectory);
  const pages = allFiles
    .filter((path) => extname(path) === ".html")
    .map((path) => ({ path, route: routeForHtml(path) }))
    .filter(({ route }) => route !== null);
  const htmlCache = new Map();

  async function html(path) {
    if (!htmlCache.has(path)) htmlCache.set(path, await readFile(path, "utf8"));
    return htmlCache.get(path);
  }

  async function referenceIssue(reference, sourceRoute) {
    let url;
    try {
      url = new URL(reference.replace(/&amp;/g, "&"), `${origin}${sourceRoute}`);
    } catch {
      return `invalid URL ${reference}`;
    }
    if (!/^https?:$/.test(url.protocol) || url.origin !== origin) return null;
    const target = await builtTarget(url.pathname);
    if (!target) return `missing target ${url.pathname}`;
    if (url.hash && extname(target) === ".html") {
      const ids = new Set(
        [...(await html(target)).matchAll(/\bid=(?:"([^"]+)"|'([^']+)')/gi)]
          .map((match) => match[1] ?? match[2]),
      );
      if (!ids.has(decode(url.hash.slice(1)))) {
        return `missing anchor ${url.hash}`;
      }
    }
    return null;
  }

  const brokenLinks = new Set();
  for (const page of pages) {
    const references = [
      ...(await html(page.path)).matchAll(
        /\b(?:href|src)=(?:"([^"]+)"|'([^']+)')/gi,
      ),
    ].map((match) => match[1] ?? match[2]);
    for (const reference of references) {
      const issue = await referenceIssue(reference, page.route);
      if (issue) {
        const source = relative(outputDirectory, page.path).split(sep).join("/");
        brokenLinks.add(`${source}: ${reference} (${issue})`);
      }
    }
  }
  check(
    "Built HTML internal links",
    [...brokenLinks],
    `${pages.length} pages crawled; no missing files or anchors`,
  );

  const sidebarLinks = collectLinks(themeConfig.sidebar);
  for (const locale of Object.values(themeConfig.locales ?? {})) {
    collectLinks(locale?.sidebar, sidebarLinks);
  }
  for (const locale of Object.values(userConfig.locales ?? {})) {
    collectLinks(locale?.themeConfig?.sidebar, sidebarLinks);
  }
  const sidebarIssues = [];
  for (const link of sidebarLinks) {
    const issue = await referenceIssue(link, "/");
    if (issue) sidebarIssues.push(`${link} (${issue})`);
  }
  check(
    "Sidebar targets",
    sidebarIssues,
    `${sidebarLinks.size} configured links resolve`,
  );

  const sitemapPath = resolve(outputDirectory, "sitemap.xml");
  const sitemapIssues = [];
  if (!(await isFile(sitemapPath))) {
    sitemapIssues.push("missing .vitepress/dist/sitemap.xml");
  } else {
    const sitemap = await readFile(sitemapPath, "utf8");
    const sitemapRoutes = new Set(
      [...sitemap.matchAll(/<loc>([^<]+)<\/loc>/g)].map((match) =>
        canonical(new URL(match[1].replace(/&amp;/g, "&")).pathname),
      ),
    );
    const requiredRoutes = new Set([
      ...pages.map(({ route }) => canonical(route)),
      ...[...sidebarLinks]
        .filter((link) => link.startsWith("/"))
        .map((link) => canonical(new URL(link, origin).pathname)),
    ]);
    for (const route of requiredRoutes) {
      if (!sitemapRoutes.has(route)) sitemapIssues.push(`missing route ${route}`);
    }
    check(
      "Sitemap",
      sitemapIssues,
      `${sitemapRoutes.size} URLs cover every sidebar and built page route`,
    );
  }
  if (!(await isFile(sitemapPath))) check("Sitemap", sitemapIssues);

  const searchIndexes = allFiles.filter((path) =>
    /@localSearchIndex[^/\\]*\.js$/.test(path),
  );
  check(
    "Local search",
    [
      ...(themeConfig.search?.provider === "local"
        ? []
        : [".vitepress/config.mts does not select the local provider"]),
      ...(searchIndexes.length ? [] : ["no built @localSearchIndex*.js asset"]),
    ],
    `${searchIndexes.length} built index asset(s)`,
  );

  const assets = {
    async fetch(request) {
      const path = exactOutputPath(new URL(request.url).pathname);
      if (!path || !(await isFile(path))) return new Response(null, { status: 404 });
      return new Response(request.method === "HEAD" ? null : "fixture", {
        status: 200,
        headers: {
          "Content-Type": extname(path) === ".html"
            ? "text/html; charset=utf-8"
            : "application/octet-stream",
        },
      });
    },
  };
  const workerRoutes = new Set(pages.map(({ route }) => route));
  for (const route of [...workerRoutes]) {
    if (route !== "/" && route.endsWith("/")) workerRoutes.add(route.slice(0, -1));
  }
  const workerIssues = [];
  for (const route of workerRoutes) {
    const response = await worker.fetch(new Request(`${origin}${route}`), {
      ASSETS: assets,
    });
    if (response.status !== 200) workerIssues.push(`${route} returned ${response.status}`);
    if (response.headers.get("Cache-Control") !== "no-cache") {
      workerIssues.push(`${route} lost HTML no-cache headers`);
    }
  }
  const headRoute = pages.find(({ route }) => route !== "/")?.route ?? "/";
  const head = await worker.fetch(
    new Request(`${origin}${headRoute}`, { method: "HEAD" }),
    { ASSETS: assets },
  );
  if (head.status !== 200) workerIssues.push(`HEAD ${headRoute} returned ${head.status}`);
  const missing = await worker.fetch(
    new Request(`${origin}/__docs_qa_missing__`),
    { ASSETS: assets },
  );
  if (missing.status !== 404) workerIssues.push("unknown clean URL did not remain 404");
  if (config.cleanUrls !== true) workerIssues.push("cleanUrls is not true");
  check(
    "Worker clean URLs",
    workerIssues,
    `${workerRoutes.size} GET routes plus HEAD and 404 behavior`,
  );

  const plannedPages = [
    {
      label: "English locale",
      route: "/en/",
      sources: ["en/index.md"],
      configured: Boolean(userConfig.locales?.en || themeConfig.locales?.en),
    },
    {
      label: "Downloads page",
      route: "/downloads",
      sources: ["downloads.md", "downloads/index.md"],
    },
    {
      label: "Changelog page",
      route: "/changelog",
      sources: ["changelog.md", "changelog/index.md"],
    },
  ];
  for (const page of plannedPages) {
    const issues = [];
    const sources = await Promise.all(
      page.sources.map((path) => isFile(resolve(docsDirectory, path))),
    );
    if (!sources.some(Boolean)) issues.push(`missing ${page.sources.join(" or ")}`);
    if (!(await builtTarget(page.route))) issues.push(`missing built route ${page.route}`);
    if (page.configured === false) {
      issues.push("missing .vitepress/config.mts locales.en");
    }
    if (issues.length) {
      plannedFailures.push(page.label);
      result("FAIL (planned)", page.label, issues.join("; "));
    } else {
      result("PASS", page.label, `${page.route} required source/config and output present`);
    }
  }

  if (hardFailures.length || (requirePlanned && plannedFailures.length)) {
    process.exitCode = 1;
    console.error(
      `\nDocs QA failed: ${hardFailures.length} integrity failure(s), ` +
        `${plannedFailures.length} planned-content failure(s)` +
        (requirePlanned ? " (strict gate enabled)." : "."),
    );
  } else {
    console.log("\nDocs QA integrity checks passed.");
    if (plannedFailures.length) {
      console.log(
        `${plannedFailures.length} planned-content FAIL(s) are report-only on current main. ` +
          "Run npm run check:docs:required to enforce them.",
      );
    }
  }
}

await main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
