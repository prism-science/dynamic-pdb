import { readFileSync } from "node:fs";
import path from "node:path";

const modelTemplate = readFileSync(path.join(__dirname, "../fixtures/model.pdb"), "utf8");

export type UploadFile = { name: string; mimeType: string; buffer: Buffer };

/**
 * A small coordinate file whose bytes are unique to tag. The model form sends
 * the file's sha256 as the model's idempotency key, and the backend returns an
 * existing model for a key it has seen, so every test needs its own bytes.
 */
export function coordinateFile(tag: string): UploadFile {
  return {
    name: `model-${tag}.pdb`,
    mimeType: "chemical/x-pdb",
    buffer: Buffer.from(`REMARK 999 BROWSER TEST ${tag}\n${modelTemplate}`, "utf8"),
  };
}

/** A stand-in for processed reflections, used as a run's input. Nothing parses it. */
export function reflectionsFile(tag: string): UploadFile {
  return {
    name: `data-${tag}.mtz`,
    mimeType: "application/octet-stream",
    buffer: Buffer.from(`browser test reflections ${tag}\n`, "utf8"),
  };
}

/**
 * An mmCIF whose header states its authors and R-factors, as a refinement
 * program writes them. Its bytes are unique to tag, like coordinateFile's.
 * name sets the file name; scores are R-work and R-free.
 */
export function scoredCoordinateFile(
  tag: string,
  name: string,
  scores: { rWork: string; rFree: string },
): UploadFile {
  const header = [
    `data_${tag.replace(/[^A-Za-z0-9]/g, "")}`,
    `_audit_author.name 'Browser, T.'`,
    `_refine.ls_R_factor_R_work ${scores.rWork}`,
    `_refine.ls_R_factor_R_free ${scores.rFree}`,
    "#",
    "",
  ].join("\n");
  return { name, mimeType: "chemical/x-mmcif", buffer: Buffer.from(header, "utf8") };
}

/**
 * An mmCIF a depositor uploads with an entry: how it was measured, and the
 * PDB ID it names when entryId is given. Its bytes are unique to tag.
 */
export function entryCoordinateFile(
  tag: string,
  name: string,
  header: { entryId?: string; resolution: string; method: string; spaceGroup: string },
): UploadFile {
  const lines = [
    header.entryId ? `data_${header.entryId}` : `data_deposit${tag.replace(/[^A-Za-z0-9]/g, "")}`,
    ...(header.entryId ? [`_entry.id ${header.entryId}`] : []),
    `_exptl.method '${header.method}'`,
    `_refine.ls_d_res_high ${header.resolution}`,
    `_symmetry.space_group_name_H-M '${header.spaceGroup}'`,
    "#",
    "",
  ];
  return { name, mimeType: "chemical/x-mmcif", buffer: Buffer.from(lines.join("\n"), "utf8") };
}
