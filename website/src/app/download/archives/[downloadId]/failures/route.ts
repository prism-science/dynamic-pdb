import { NextResponse } from "next/server";

import {
  downloadProblem,
  isDownloadId,
  listFailures,
  problemHttpStatus,
} from "@/lib/api/downloads";

export const dynamic = "force-dynamic";

/**
 * One page of the files a ready archive could not vouch for, for the panel's
 * failures list: GET /v1/downloads/{id}/failures through the website server,
 * with the page's cursor. A failure comes back as a problem the panel can
 * explain, with the backend's status.
 */
export async function GET(
  request: Request,
  { params }: { params: Promise<{ downloadId: string }> },
) {
  const { downloadId } = await params;
  if (!isDownloadId(downloadId)) {
    return NextResponse.json(
      { problem: { code: "not_found", detail: null, retryAfterSeconds: null } },
      { status: 404 },
    );
  }
  const cursor = new URL(request.url).searchParams.get("cursor") || null;
  try {
    return NextResponse.json(await listFailures(downloadId, cursor));
  } catch (error) {
    const problem = downloadProblem(error);
    return NextResponse.json({ problem }, { status: problemHttpStatus(problem.code) });
  }
}
