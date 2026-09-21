// Artifact proxy for the heterogeneity viewer: /dpdb-file?u=<encoded https uri>.
//
// Deliberately NOT under /api. The ingress routes /api by prefix straight to the
// Go backend and strips the prefix, so a Next route under /api is unreachable in
// production.
//
// The viewer downloads coordinates and structure factors in the browser. The
// configured files host sends no CORS headers, so those fetches have to come
// from this origin. Redirects (the /api/v1/files shortcut) are followed
// server-side.

import { proxyUpstream } from "@/lib/dpdb-proxy";

export const dynamic = "force-dynamic";

function allowedHosts(): Set<string> {
  const hosts = new Set(["files.dynamicpdb.com", "dynamicpdb.com"]);
  for (const value of [
    process.env.DYNAMIC_PDB_FILE_BASE_URL,
    process.env.APP_BASE_URL,
  ]) {
    if (!value) continue;
    try {
      const configured = new URL(value);
      if (configured.protocol === "https:") hosts.add(configured.host);
    } catch {
      // Invalid deployment configuration must not expand the proxy allowlist.
    }
  }
  return hosts;
}

export async function GET(request: Request) {
  let target: URL;
  try {
    target = new URL(new URL(request.url).searchParams.get("u") ?? "");
  } catch {
    return new Response("missing or invalid u", { status: 400 });
  }
  if (target.protocol !== "https:" || !allowedHosts().has(target.host)) {
    return new Response("host not allowed", { status: 403 });
  }
  return proxyUpstream(target.toString());
}
