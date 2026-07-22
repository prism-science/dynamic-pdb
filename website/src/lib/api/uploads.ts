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

export type UploadProgress = (fraction: number) => void;

export async function uploadFileToObjectStorage(
  file: File,
  context: FileUploadContext,
  onProgress?: UploadProgress,
): Promise<string> {
  const grant = await postJSON<FileUploadGrant>("/api/files", {
    entry_id: context.entryId,
    experiment_id: context.experimentId ?? null,
    entity_id: context.entityId,
    filename: context.filename ?? file.name,
    size: file.size,
  });
  validateFileUploadGrant(grant);

  const totalBytes = file.size > 0 ? file.size : 1;
  let uploadedBytes = 0;
  const completed: CompletedFileUploadPart[] = [];
  try {
    for (const part of grant.parts) {
      const start = (part.part_number - 1) * grant.part_size;
      const end = Math.min(start + grant.part_size, file.size);
      const partBytes = end - start;
      const etag = await putPartWithProgress(
        part.url,
        file.slice(start, end),
        part.part_number,
        file.name,
        (loaded) => {
          onProgress?.(Math.min(1, (uploadedBytes + loaded) / totalBytes));
        },
      );
      uploadedBytes += partBytes;
      onProgress?.(Math.min(1, uploadedBytes / totalBytes));
      completed.push({ part_number: part.part_number, etag });
    }

    await postJSON("/api/files/complete", {
      key: grant.key,
      upload_id: grant.upload_id,
      parts: completed,
    });
    onProgress?.(1);
    return grant.object_url;
  } catch (error) {
    await abortFileUpload(grant);
    throw error;
  }
}

// Uploads a single part via XMLHttpRequest so we can surface byte-level
// progress (fetch() gives no upload progress events). Resolves with the ETag.
function putPartWithProgress(
  url: string,
  body: Blob,
  partNumber: number,
  filename: string,
  onProgress: (loadedBytes: number) => void,
): Promise<string> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", url);
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) {
        onProgress(event.loaded);
      }
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        const etag = xhr.getResponseHeader("ETag");
        if (!etag) {
          reject(new Error(`Object storage did not return ETag for ${filename}.`));
          return;
        }
        resolve(etag);
        return;
      }
      reject(
        new Error(
          `Object storage rejected part ${partNumber} of ${filename}: HTTP ${xhr.status} ${xhr.responseText}`.trim(),
        ),
      );
    };
    xhr.onerror = () => {
      reject(
        new Error(
          `Browser could not upload part ${partNumber} of ${filename} to object storage. Check S3 bucket CORS for PUT and ETag.`,
        ),
      );
    };
    xhr.send(body);
  });
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
