import type {
  FastaMetadata,
  FastaRecordMetadata,
} from "@/lib/api/entries";

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

export function fastaText(metadata: FastaMetadata): string {
  return fastaRecords(metadata)
    .map((record) =>
      record.header ? `>${record.header}\n${record.sequence}` : record.sequence,
    )
    .join("\n");
}

function normalizeSequence(sequence: string): string {
  return sequence.replace(/\s+/g, "").toUpperCase();
}
