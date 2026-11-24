import { NextResponse } from "next/server";

import { downloadProblem, isDownloadId, problemHttpStatus, readArchive } from "@/lib/api/downloads";

export const dynamic = "force-dynamic";

/**
 * Where an archive request stands, for the download panel to poll:
 * GET /v1/downloads/{id} through the website server. A failure comes back as
 * a problem the panel can explain, with the backend's status.
 */
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ downloadId: string }> },
) {
  const { downloadId } = await params;
  if (!isDownloadId(downloadId)) {
    return NextResponse.json(
      { problem: { code: "not_found", detail: null, retryAfterSeconds: null } },
      { status: 404 },
    );
  }
  try {
    return NextResponse.json({ download: await readArchive(downloadId) });
  } catch (error) {
    const problem = downloadProblem(error);
    return NextResponse.json({ problem }, { status: problemHttpStatus(problem.code) });
  }
}
