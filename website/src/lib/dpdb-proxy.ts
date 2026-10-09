// Server-only: forwards one upstream GET and streams the body back.
//
// Two of the hosts the heterogeneity viewer reads from cannot be reached by a
// browser directly. files.dynamicpdb.com (S3 behind CloudFront) sends no CORS
// headers at all, and the catalogue API allows only the https://dynamicpdb.com
// origin, so `next dev` on localhost cannot call it either. The two proxy routes
// under src/app funnel through here.
//
// The file half is a stopgap. Once CloudFront sends CORS for this origin, hand
// the viewer an empty `fileProxy` and delete /dpdb-file.

// The backend counts /api/v1/files requests as downloads for analytics. It
// skips this user agent: these are viewer loads, already counted as such.
export const proxyUserAgent = "dynamic-pdb-website-proxy";

export async function proxyUpstream(url: string): Promise<Response> {
  let upstream: Response;
  try {
    upstream = await fetch(url, {
      redirect: "follow",
      cache: "no-store",
      headers: { "user-agent": proxyUserAgent },
    });
  } catch {
    // DNS failure, refused connection, a dropped transfer: an unhandled throw
    // here answers the browser with a 500 and a stack trace, which tells the
    // reader nothing and the logs no more than this does.
    return new Response("upstream unreachable", {
      status: 502,
      headers: { "cache-control": "no-store" },
    });
  }
  // Node's fetch has already decoded any gzip, so forwarding content-length or
  // content-encoding would describe a body that no longer exists.
  const headers = new Headers({ "cache-control": "no-store" });
  const contentType = upstream.headers.get("content-type");
  if (contentType) headers.set("content-type", contentType);
  return new Response(upstream.body, { status: upstream.status, headers });
}
