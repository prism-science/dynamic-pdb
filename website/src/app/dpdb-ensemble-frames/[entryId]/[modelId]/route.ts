// The viewer's copy of an ensemble deposited as altlocs: /dpdb-ensemble-frames/<entry>/<model>.
// See lib/altloc-ensemble. Not under /api, for the same reason as /dpdb-file: the ingress owns it.

import { altlocEnsembleFile } from "@/lib/altloc-ensemble";
import { ensembleCoordinateText } from "@/lib/ensemble-view";

export const dynamic = "force-dynamic";

export async function GET(
  _request: Request,
  { params }: { params: Promise<{ entryId: string; modelId: string }> },
) {
  const { entryId, modelId } = await params;
  const text = await ensembleCoordinateText(entryId, modelId);
  if (text === null) {
    return new Response("no ensemble file for this model", { status: 404 });
  }
  const file = altlocEnsembleFile(text);
  if (file.kind === "not-an-altloc-ensemble") {
    return new Response("the file's altloc letters do not cover the whole chain", { status: 404 });
  }
  return new Response(file.kind === "converted" ? file.text : text, {
    headers: { "cache-control": "no-store", "content-type": "chemical/x-mmcif" },
  });
}
