import type {
  FastaMetadata,
  FastaRecordMetadata,
} from "@/lib/api/entries";

export const FASTA_LINE = 60;

// Residues are read in blocks of ten, sixty to a line, as every sequence
// database prints them.
const RESIDUE_GROUP = 10;

export function fastaRecords(metadata: FastaMetadata): FastaRecordMetadata[] {
  if (!Array.isArray(metadata.records)) {
    return [];
  }
  return metadata.records.flatMap((record) => {
    if (!record || typeof record.sequence !== "string") {
      return [];
    }
    const sequence = plainSequence(record.sequence);
    if (!sequence) {
      return [];
    }
    return [
      {
        header: typeof record.header === "string" ? record.header.trim() : "",
        sequence,
      },
    ];
  });
}

export function fastaTotalLength(metadata: FastaMetadata): number {
  return fastaRecords(metadata).reduce(
    (total, record) => total + record.sequence.length,
    0,
  );
}

// A single record written back out as FASTA, wrapped at 60 columns like every
// tool that produces one, so the result can be pasted straight into an
// alignment.
export function fastaRecordText(
  record: FastaRecordMetadata,
  fallbackHeader = "",
): string {
  const sequence = plainSequence(record.sequence);
  const lines: string[] = [];
  for (let offset = 0; offset < sequence.length; offset += FASTA_LINE) {
    lines.push(sequence.slice(offset, offset + FASTA_LINE));
  }
  return `>${record.header || fallbackHeader}\n${lines.join("\n")}\n`;
}

// The same sequence laid out for reading rather than for copying: sixty
// residues to a line, spaced into groups of ten.
export function sequenceLines(sequence: string): string[] {
  const residues = plainSequence(sequence);
  const lines: string[] = [];
  for (let offset = 0; offset < residues.length; offset += FASTA_LINE) {
    const slice = residues.slice(offset, offset + FASTA_LINE);
    const groups: string[] = [];
    for (let position = 0; position < slice.length; position += RESIDUE_GROUP) {
      groups.push(slice.slice(position, position + RESIDUE_GROUP));
    }
    lines.push(groups.join(" "));
  }
  return lines;
}

/** Residues only: whitespace stripped and upper-cased, the form a search box or
 *  an alignment tool expects. */
export function plainSequence(sequence: string): string {
  return sequence.replace(/\s+/g, "").toUpperCase();
}
