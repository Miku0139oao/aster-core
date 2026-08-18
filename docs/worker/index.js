function withSecurityHeaders(response, pathname) {
  const headers = new Headers(response.headers);
  headers.set("X-Content-Type-Options", "nosniff");
  headers.set("Referrer-Policy", "strict-origin-when-cross-origin");
  headers.set("X-Frame-Options", "SAMEORIGIN");
  headers.set(
    "Permissions-Policy",
    "camera=(), geolocation=(), microphone=()",
  );

  const contentType = headers.get("Content-Type") || "";
  if (contentType.includes("text/html")) {
    headers.set("Cache-Control", "no-cache");
  } else if (pathname.startsWith("/assets/")) {
    headers.set("Cache-Control", "public, max-age=31536000, immutable");
  }

  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers,
  });
}

async function fetchAsset(request, env, pathname) {
  const url = new URL(request.url);
  url.pathname = pathname;
  return env.ASSETS.fetch(new Request(url, request));
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    let response = await env.ASSETS.fetch(request);

    if (
      response.status === 404 &&
      (request.method === "GET" || request.method === "HEAD") &&
      !url.pathname.split("/").at(-1)?.includes(".")
    ) {
      const candidates = url.pathname.endsWith("/")
        ? url.pathname === "/"
          ? ["index.html"]
          : [
              `${url.pathname}index.html`,
              `${url.pathname.replace(/\/$/, "")}.html`,
            ]
        : [`${url.pathname}.html`, `${url.pathname}/index.html`];

      for (const candidate of candidates) {
        response = await fetchAsset(request, env, candidate);
        if (response.status !== 404) {
          break;
        }
      }
    }

    return withSecurityHeaders(response, url.pathname);
  },
};
