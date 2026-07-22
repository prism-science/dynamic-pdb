export type FileUploadContext = {
  entryId: string;
  experimentId?: string | null;
  entityId: string;
  filename?: string;
};

type FileUploadGrant = {
  key: string;
  upload_id: string;
  object_url: string;
  part_size: number;
  parts: FileUploadPart[];
};

type FileUploadPart = {
  part_number: number;
  url: string;
};

type CompletedFileUploadPart = {
  part_number: number;
  etag: string;
};

export async function uploadFileToObjectStorage(
  file: File,
  context: FileUploadContext,
): Promise<string> {
  const grant = await postJSON<FileUploadGrant>("/api/files", {
    entry_id: context.entryId,
    experiment_id: context.experimentId ?? null,
    entity_id: context.entityId,
    filename: context.filename ?? file.name,
    size: file.size,
  });
  validateFileUploadGrant(grant);

  const completed: CompletedFileUploadPart[] = [];
  try {
    for (const part of grant.parts) {
      const start = (part.part_number - 1) * grant.part_size;
      const end = Math.min(start + grant.part_size, file.size);
      let response: Response;
      try {
        response = await fetch(part.url, {
          method: "PUT",
          body: file.slice(start, end),
        });
      } catch (error) {
        throw new Error(
          `Browser could not upload part ${part.part_number} of ${file.name} to object storage. Check S3 bucket CORS for PUT and ETag. ${
            error instanceof Error ? error.message : "Upload request failed."
          }`,
        );
      }
      if (!response.ok) {
        throw new Error(
          `Object storage rejected part ${part.part_number} of ${file.name}: ${await responseError(response)}`,
        );
      }
      const etag = response.headers.get("ETag");
      if (!etag) {
        throw new Error(`Object storage did not return ETag for ${file.name}.`);
      }
      completed.push({ part_number: part.part_number, etag });
    }

    await postJSON("/api/files/complete", {
      key: grant.key,
      upload_id: grant.upload_id,
      parts: completed,
    });
    return grant.object_url;
  } catch (error) {
    await abortFileUpload(grant);
    throw error;
  }
}

async function abortFileUpload(grant: FileUploadGrant): Promise<void> {
  try {
    await postJSON("/api/files/abort", {
      key: grant.key,
      upload_id: grant.upload_id,
    });
  } catch {
    // Object storage expires abandoned uploads; keep the original upload error.
  }
}

async function postJSON<T = unknown>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method: "POST",
    cache: "no-store",
    headers:
      body === undefined
        ? { Accept: "application/json" }
        : {
            Accept: "application/json",
            "Content-Type": "application/json",
          },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) {
    throw new Error(await responseError(response));
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

async function responseError(response: Response): Promise<string> {
  let text = "";
  try {
    text = await response.text();
  } catch {
    text = "";
  }
  if (!text) {
    return `HTTP ${response.status}`;
  }
  try {
    const body = JSON.parse(text) as { error?: string; message?: string };
    return body.error ?? body.message ?? `HTTP ${response.status}`;
  } catch {
    return text;
  }
}

function validateFileUploadGrant(grant: FileUploadGrant): void {
  if (
    !grant ||
    typeof grant.key !== "string" ||
    typeof grant.upload_id !== "string" ||
    typeof grant.object_url !== "string" ||
    typeof grant.part_size !== "number" ||
    grant.part_size <= 0 ||
    !Array.isArray(grant.parts) ||
    grant.parts.length === 0
  ) {
    throw new Error("Backend returned an invalid file upload grant.");
  }
}
