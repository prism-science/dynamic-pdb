import { headers } from "next/headers";

import { getApiBaseUrl } from "@/lib/api/baseUrl";

/** Where the heterogeneity viewer should send its catalogue calls and file downloads. */
export interface HetstarDpdbConfig {
  apiBase: string;
  fileProxy: string;
}

/**
 * Artifacts always go through our own proxy: files.dynamicpdb.com is a different
 * origin from the site however the site is served, and it sends no CORS headers
 * at all. This is the one to revisit -- a CloudFront rule for
 * https://dynamicpdb.com would let this be "" and take the website pod out of the
 * path of structure-factor downloads that reach hundreds of megabytes.
 */
const FILE_PROXY = "/dpdb-file";

/**
 * The viewer fetches from the browser, so what it can reach depends on where the
 * page was served from.
 *
 * Served from the same origin as the API -- dynamicpdb.com, where the ingress
 * puts the backend under /api -- it calls the catalogue directly. Served from
 * anywhere else (`next dev` on localhost, a tunnel, a preview host) that call is
 * cross-origin and the API allows only https://dynamicpdb.com, so it goes through
 * our proxy route instead.
 *
 * Exported for the tests; `hetstarDpdbConfig` is what pages call.
 */
export function resolveDpdbConfig(
  apiBaseUrl: string,
  requestOrigin: string | null,
): HetstarDpdbConfig {
  let configured: URL;
  try {
    configured = new URL(apiBaseUrl);
  } catch {
    return { apiBase: "/dpdb-api/v1", fileProxy: FILE_PROXY };
  }
  const sameOrigin = requestOrigin !== null && requestOrigin === configured.origin;
  const apiPath = configured.pathname.replace(/\/+$/, "");
  return {
    apiBase: sameOrigin ? `${apiPath}/v1` : "/dpdb-api/v1",
    fileProxy: FILE_PROXY,
  };
}

function originOf(headerList: Headers): string | null {
  const proto = headerList.get("x-forwarded-proto")?.split(",")[0]?.trim();
  const host =
    headerList.get("x-forwarded-host")?.split(",")[0]?.trim() ??
    headerList.get("host");
  if (!host) return null;
  return `${proto || "http"}://${host}`;
}

export async function hetstarDpdbConfig(): Promise<HetstarDpdbConfig> {
  return resolveDpdbConfig(getApiBaseUrl(), originOf(await headers()));
}
