import { NextResponse } from "next/server";

import {
  downloadProblem,
  isDownloadId,
  listSelectionFileChecks,
  problemHttpStatus,
} from "@/lib/api/downloads";

export const dynamic = "force-dynamic";

/**
 * One page of a saved selection's models with each file's last check, for the
 * drawer (researchers' Q10): GET /v1/selections/{id}/models through the website
 * server, with the page's cursor. A failure comes back as a problem the panel
 * can explain, with the backend's status.
 */
export async function GET(
  request: Request,
  { params }: { params: Promise<{ selectionId: string }> },
) {
  const { selectionId } = await params;
  if (!isDownloadId(selectionId)) {
    return NextResponse.json(
      { problem: { code: "not_found", detail: null, retryAfterSeconds: null } },
      { status: 404 },
    );
  }
  const cursor = new URL(request.url).searchParams.get("cursor") || null;
  try {
    return NextResponse.json(await listSelectionFileChecks(selectionId, cursor));
  } catch (error) {
    const problem = downloadProblem(error);
    return NextResponse.json({ problem }, { status: problemHttpStatus(problem.code) });
  }
}
