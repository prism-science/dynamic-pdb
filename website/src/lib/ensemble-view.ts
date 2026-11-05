// Server-only: what the viewer's two ensemble routes share. Everything about the model and its file
// comes from the catalogue, so a request cannot make the site fetch a URL of its choosing.

import { getApiBaseUrl } from "@/lib/api/baseUrl";
import { backendReadTimeoutMs, fetchWithTimeout } from "@/lib/api/timeout";
import { proxyUserAgent } from "@/lib/dpdb-proxy";

type CatalogueArtifact = {
  id: string;
  attributes: { type?: string; uri?: string | null; format?: string | null; sha256?: string | null };
};

export type ModelArtifactsDocument = { data: CatalogueArtifact[] };

type ModelDocument = {
  data: { attributes: { primary_artifact_id?: string | null; metadata?: Record<string, unknown> } };
};

/** Above this the viewer's own download prompt applies, and the site does not rewrite the file. */
const MAX_REWRITTEN_FILE_BYTES = 200 * 1024 * 1024;

function fileHosts(): Set<string> {
  const hosts = new Set(["files.rcsb.org", "files.dynamicpdb.com"]);
  const configured = process.env.DYNAMIC_PDB_FILE_BASE_URL;
  if (configured) {
    try {
      hosts.add(new URL(configured).host);
    } catch {
      // Invalid deployment configuration must not expand the list.
    }
  }
  return hosts;
}

function catalogueURL(path: string): string {
  return `${getApiBaseUrl().replace(/\/+$/, "")}/${path}`;
}

async function catalogueJSON<Document>(path: string): Promise<Document | null> {
  const response = await fetchWithTimeout(
    catalogueURL(path),
    { cache: "no-store", headers: { "user-agent": proxyUserAgent } },
    backendReadTimeoutMs,
  );
  return response.ok ? ((await response.json()) as Document) : null;
}

export function modelArtifactsPath(entryId: string, modelId: string): string {
  return `v1/entries/${encodeURIComponent(entryId)}/models/${encodeURIComponent(modelId)}/artifacts`;
}

export function ensembleFramesPath(entryId: string, modelId: string): string {
  return `/dpdb-ensemble-frames/${encodeURIComponent(entryId)}/${encodeURIComponent(modelId)}`;
}

/** The model's coordinate file, picked the way the viewer picks it. */
export function coordinateArtifact(
  artifacts: CatalogueArtifact[],
  primaryArtifactId: string | null | undefined,
): CatalogueArtifact | null {
  return (
    artifacts.find((artifact) => artifact.attributes.type === "model" && artifact.id === primaryArtifactId) ??
    artifacts.find((artifact) => artifact.attributes.type === "model") ??
    null
  );
}

async function loadEnsembleModel(entryId: string, modelId: string) {
  const model = await catalogueJSON<ModelDocument>(
    `v1/entries/${encodeURIComponent(entryId)}/models/${encodeURIComponent(modelId)}`,
  );
  return model?.data.attributes.metadata?.model_type === "Ensemble" ? model.data.attributes : null;
}

export async function primaryArtifactIdOfEnsemble(entryId: string, modelId: string): Promise<string | null | undefined> {
  const model = await loadEnsembleModel(entryId, modelId);
  return model ? (model.primary_artifact_id ?? null) : undefined;
}

/** The text of an Ensemble model's coordinate file, or null when it has none the site may read. */
export async function ensembleCoordinateText(entryId: string, modelId: string): Promise<string | null> {
  const model = await loadEnsembleModel(entryId, modelId);
  if (!model) {
    return null;
  }
  const artifacts = await catalogueJSON<ModelArtifactsDocument>(modelArtifactsPath(entryId, modelId));
  const artifact = artifacts && coordinateArtifact(artifacts.data, model.primary_artifact_id);
  const uri = artifact?.attributes.uri;
  if (!uri || !/^(cif|mmcif)$/i.test(artifact.attributes.format ?? "")) {
    return null;
  }
  const target = new URL(uri);
  if (target.protocol !== "https:" || !fileHosts().has(target.host)) {
    return null;
  }
  const response = await fetchWithTimeout(
    target.toString(),
    { redirect: "follow", cache: "no-store", headers: { "user-agent": proxyUserAgent } },
    backendReadTimeoutMs,
  );
  if (!response.ok) {
    return null;
  }
  const size = Number(response.headers.get("content-length") ?? 0);
  if (size > MAX_REWRITTEN_FILE_BYTES) {
    return null;
  }
  return response.text();
}
