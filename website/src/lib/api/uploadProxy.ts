import "server-only";

import { NextRequest, NextResponse } from "next/server";

import { getApiBaseUrl, jsonApiMediaType } from "@/lib/api/baseUrl";
import { getAuthSession } from "@/lib/auth/session";

export async function proxyUploadControlRequest(
  request: NextRequest,
  backendPath: string,
): Promise<NextResponse> {
  const session = await getAuthSession();
  if (!session) {
    return NextResponse.json(
      { error: "You must be signed in to upload files." },
      { status: 401 },
    );
  }

  let body: string | undefined;
  try {
    body = request.body == null ? undefined : await request.text();
  } catch {
    return NextResponse.json(
      { error: "Invalid request body." },
      { status: 400 },
    );
  }

  const headers: Record<string, string> = {
    Accept: jsonApiMediaType,
    Authorization: `Bearer ${session.token}`,
  };
  if (body != null && body.length > 0) {
    headers["Content-Type"] =
      request.headers.get("content-type") ?? jsonApiMediaType;
  }

  let response: Response;
  try {
    response = await fetch(
      `${getApiBaseUrl().replace(/\/+$/, "")}${backendPath}`,
      {
        method: request.method,
        cache: "no-store",
        headers,
        body,
      },
    );
  } catch {
    return NextResponse.json(
      { error: "Could not reach backend upload endpoint." },
      { status: 502 },
    );
  }

  if (response.status === 204) {
    return new NextResponse(null, { status: 204 });
  }

  const responseBody = await response.text();
  const responseHeaders = new Headers();
  const contentType = response.headers.get("content-type");
  if (contentType) {
    responseHeaders.set("content-type", contentType);
  }
  return new NextResponse(responseBody, {
    status: response.status,
    headers: responseHeaders,
  });
}
