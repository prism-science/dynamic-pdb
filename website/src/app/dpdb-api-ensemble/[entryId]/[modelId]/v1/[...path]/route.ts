// The catalogue as the viewer sees it for an ensemble deposited as altlocs. Every call passes
// through, except that the model's own file becomes the site's frames of it (lib/altloc-ensemble).
// The file's hash is dropped there: it names the deposited file, not the frames.
//
// Not under /api, for the same reason as /dpdb-file: the ingress owns that prefix.

import { getApiBaseUrl } from "@/lib/api/baseUrl";
import { proxyUpstream } from "@/lib/dpdb-proxy";
import {
  coordinateArtifact,
  ensembleFramesPath,
  modelArtifactsPath,
  primaryArtifactIdOfEnsemble,
  type ModelArtifactsDocument,
} from "@/lib/ensemble-view";

export const dynamic = "force-dynamic";

export async function GET(
  request: Request,
  { params }: { params: Promise<{ entryId: string; modelId: string; path: string[] }> },
) {
  const { entryId, modelId, path } = await params;
  if (path.some((segment) => segment === "" || segment === "..")) {
    return new Response("bad path", { status: 400 });
  }
  const search = new URL(request.url).search;
  const base = getApiBaseUrl().replace(/\/+$/, "");
  const upstream = await proxyUpstream(`${base}/v1/${path.map(encodeURIComponent).join("/")}${search}`);
  if (`v1/${path.join("/")}` !== modelArtifactsPath(entryId, modelId) || !upstream.ok) {
    return upstream;
  }
  const primaryArtifactId = await primaryArtifactIdOfEnsemble(entryId, modelId);
  if (primaryArtifactId === undefined) {
    return upstream;
  }
  const document = (await upstream.json()) as ModelArtifactsDocument;
  const artifact = coordinateArtifact(document.data, primaryArtifactId);
  if (artifact) {
    artifact.attributes.uri = ensembleFramesPath(entryId, modelId);
    artifact.attributes.sha256 = null;
  }
  return Response.json(document, { headers: { "cache-control": "no-store" } });
}
