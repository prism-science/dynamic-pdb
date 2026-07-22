import type { NextRequest } from "next/server";

import { proxyUploadControlRequest } from "@/lib/api/uploadProxy";

export async function POST(request: NextRequest) {
  return proxyUploadControlRequest(request, "/v1/files/abort");
}
