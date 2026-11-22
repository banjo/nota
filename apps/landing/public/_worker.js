// Cloudflare Pages advanced mode: one deploy serves the landing page at
// / and the web app at /app/.
//
// Pages answers a missing path with the site's index.html (the landing)
// rather than a 404, so the worker cannot key on the status. A path
// under /app without a file extension is the web app's route (it has no
// dots), and gets app/index.html; everything with an extension is a real
// file and is served as uploaded. (_redirects cannot express this: a
// /app/* rewrite also catches /app/assets/*.)
export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const isAppRoute =
      (url.pathname === "/app" || url.pathname.startsWith("/app/")) &&
      !/\.[a-zA-Z0-9]+$/.test(url.pathname);
    if (!isAppRoute) return env.ASSETS.fetch(request);
    const index = await env.ASSETS.fetch(new URL("/app/", url).toString());
    return new Response(request.method === "HEAD" ? null : index.body, {
      status: index.status,
      headers: {
        "content-type": "text/html; charset=utf-8",
        "cache-control": "public, max-age=0, must-revalidate",
        "x-content-type-options": "nosniff",
        "referrer-policy": "strict-origin-when-cross-origin",
      },
    });
  },
};
