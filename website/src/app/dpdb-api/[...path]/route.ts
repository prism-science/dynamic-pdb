// Catalogue proxy for the heterogeneity viewer: /dpdb-api/<path> -> <api base>/<path>.
//
// Only reached when the configured API base is a different origin from the page,
// which in practice means `next dev` on localhost against the deployed backend:
// that API allows the https://dynamicpdb.com origin only. On dynamicpdb.com the
// viewer is pointed straight at /api/v1 and never calls this route.
//
// Not under /api, for the same reason as /dpdb-file: the ingress owns that prefix.

import { getApiBaseUrl } from "@/lib/api/baseUrl";
import { proxyUpstream } from "@/lib/dpdb-proxy";

export const dynamic = "force-dynamic";

export async function GET(
  request: Request,
  { params }: { params: Promise<{ path: string[] }> },
) {
  const { path } = await params;
  if (path.some((segment) => segment === "" || segment === "..")) {
    return new Response("bad path", { status: 400 });
  }
  const search = new URL(request.url).search;
  const base = getApiBaseUrl().replace(/\/+$/, "");
  return proxyUpstream(`${base}/${path.map(encodeURIComponent).join("/")}${search}`);
}
