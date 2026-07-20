export type StructureKind = "pdb" | "mmcif" | "ccp4";

// Pure helper (no "use client") so server components can call it directly.
export function detectStructureKind(
  type: string | undefined,
): StructureKind | null {
  const t = (type ?? "").toLowerCase();
  if (/(mmcif|mcif)/.test(t) || /\bcif\b/.test(t) || /\.cif/.test(t)) {
    return "mmcif";
  }
  if (/pdb/.test(t)) {
    return "pdb";
  }
  if (/(ccp4|mrc|\bmap\b|density)/.test(t)) {
    return "ccp4";
  }
  return null;
}
