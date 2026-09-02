import { readFile } from "node:fs/promises";
import path from "node:path";

import { getApiBaseUrl } from "@/lib/api/baseUrl";

export const runtime = "nodejs";

const openAPIMediaType = "application/yaml; charset=utf-8";
const localOpenAPIPaths = [
  path.resolve(process.cwd(), "..", "backend", "api", "openapi.yaml"),
  path.resolve(process.cwd(), "backend", "api", "openapi.yaml"),
];

export async function GET() {
  if (process.env.NODE_ENV !== "production") {
    const localSpec = await readLocalSpec();
    if (localSpec) {
      return yamlResponse(localSpec, "no-cache");
    }
  }

  const response = await fetch(`${getApiBaseUrl()}/openapi.yaml`, {
    headers: { Accept: "application/yaml" },
  });
  if (!response.ok) {
    return new Response("OpenAPI description is unavailable.", {
      status: response.status,
    });
  }

  const spec = await response.text();
  return yamlResponse(
    spec,
    response.headers.get("Cache-Control") ?? "public, max-age=300",
  );
}

async function readLocalSpec(): Promise<string | null> {
  for (const filePath of localOpenAPIPaths) {
    try {
      return await readFile(filePath, "utf8");
    } catch (error) {
      if (isMissingFileError(error)) {
        continue;
      }
      throw error;
    }
  }
  return null;
}

function isMissingFileError(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    error.code === "ENOENT"
  );
}

function yamlResponse(spec: string, cacheControl: string): Response {
  return new Response(spec, {
    headers: {
      "Cache-Control": cacheControl,
      "Content-Type": openAPIMediaType,
    },
  });
}
