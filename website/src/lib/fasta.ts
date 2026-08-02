import type {
  FastaMetadata,
  FastaRecordMetadata,
} from "@/lib/api/entries";

export const FASTA_LINE = 60;

export function fastaRecords(metadata: FastaMetadata): FastaRecordMetadata[] {
  if (!Array.isArray(metadata.records)) {
    return [];
  }
  return metadata.records.flatMap((record) => {
    if (!record || typeof record.sequence !== "string") {
      return [];
    }
    const sequence = normalizeSequence(record.sequence);
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
  const sequence = normalizeSequence(record.sequence);
  const lines: string[] = [];
  for (let offset = 0; offset < sequence.length; offset += FASTA_LINE) {
    lines.push(sequence.slice(offset, offset + FASTA_LINE));
  }
  return `>${record.header || fallbackHeader}\n${lines.join("\n")}\n`;
}

function normalizeSequence(sequence: string): string {
  return sequence.replace(/\s+/g, "").toUpperCase();
}
