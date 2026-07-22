"use client";

import { useRef, useState, type ChangeEvent, type FormEvent } from "react";

import type {
  CreateEntryInput,
  EntityLevel,
  FastaMetadata,
} from "@/lib/api/entries";
import {
  uploadFileToObjectStorage,
  type FileUploadContext,
} from "@/lib/api/uploads";
import { createEntryAction } from "@/app/entries/new/actions";
import SequenceView from "./SequenceView";

import styles from "./NewEntryForm.module.css";

type ParsedFile = {
  id: string;
  file: File;
  name: string;
  size: number;
  type: string;
  level: EntityLevel;
  authors: string;
  affiliation: string;
  metadata?: Record<string, unknown>;
  preview?: string;
  url: string;
};

type MetricDraft = {
  id: string;
  values: Record<string, string>;
};

type ExperimentDraft = {
  id: string;
  name: string;
  description: string;
  thumbFileId: string;
  thumbFile: File | null;
  thumbPreview: string | null;
  files: ParsedFile[];
  metrics: MetricDraft[];
};

const LEVELS: EntityLevel[] = ["L0", "L1", "L2", "L3"];

const METRIC_FIELDS: { key: string; label: string }[] = [
  { key: "r_work", label: "R-work" },
  { key: "r_free", label: "R-free" },
  { key: "rscc", label: "RSCC" },
  { key: "cc", label: "CC" },
];

export default function NewEntryForm() {
  const [entryId] = useState(() => crypto.randomUUID());
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [thumbFileId, setThumbFileId] = useState(() => crypto.randomUUID());
  const [thumbFile, setThumbFile] = useState<File | null>(null);
  const [thumbPreview, setThumbPreview] = useState<string | null>(null);
  const [files, setFiles] = useState<ParsedFile[]>([]);
  const [experiments, setExperiments] = useState<ExperimentDraft[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const onThumb = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }
    setThumbFileId(crypto.randomUUID());
    setThumbFile(file);
    setThumbPreview(URL.createObjectURL(file));
  };

  const addEntryFiles = async (list: File[]) => {
    const parsed = await Promise.all(list.map((file) => parseFile(file, "L0")));
    setFiles((prev) => [...prev, ...parsed]);
  };

  const addExperiment = () => {
    setExperiments((prev) => [
      ...prev,
      {
        id: crypto.randomUUID(),
        name: "",
        description: "",
        thumbFileId: crypto.randomUUID(),
        thumbFile: null,
        thumbPreview: null,
        files: [],
        metrics: [],
      },
    ]);
  };

  const updateExperiment = (id: string, patch: Partial<ExperimentDraft>) => {
    setExperiments((prev) =>
      prev.map((exp) => (exp.id === id ? { ...exp, ...patch } : exp)),
    );
  };

  const removeExperiment = (id: string) => {
    setExperiments((prev) => prev.filter((exp) => exp.id !== id));
  };

  const canSubmit =
    name.trim().length > 0 &&
    experiments.every((exp) => exp.name.trim().length > 0) &&
    !submitting;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setSubmitting(true);
    setError(null);

    try {
      const entryThumbnailURL = thumbFile
        ? await uploadFileToObjectStorage(thumbFile, {
            entryId,
            experimentId: null,
            entityId: thumbFileId,
          })
        : null;
      const uploadedFiles = await Promise.all(
        files.map((file) =>
          uploadParsedFile(file, { entryId, experimentId: null }),
        ),
      );
      const uploadedExperiments = await Promise.all(
        experiments.map(async (exp) => ({
          ...exp,
          thumbnailURL: exp.thumbFile
            ? await uploadFileToObjectStorage(exp.thumbFile, {
                entryId,
                experimentId: exp.id,
                entityId: exp.thumbFileId,
              })
            : null,
          files: await Promise.all(
            exp.files.map((file) =>
              uploadParsedFile(file, { entryId, experimentId: exp.id }),
            ),
          ),
        })),
      );

      const input: CreateEntryInput = {
        id: entryId,
        name: name.trim(),
        description: description.trim() || null,
        thumbnail_image_url: entryThumbnailURL,
        entities: uploadedFiles.map(toEntity),
        experiments: uploadedExperiments.map((exp) => ({
          id: exp.id,
          name: exp.name.trim(),
          description: exp.description.trim() || null,
          thumbnail_image_url: exp.thumbnailURL,
          entities: [...exp.files.map(toEntity), ...exp.metrics.map(toMetricEntity)],
        })),
      };

      const result = await createEntryAction(input);
      if (result?.error) {
        setError(result.error);
        setSubmitting(false);
      }
    } catch (error) {
      setError(
        error instanceof Error ? error.message : "Failed to upload files.",
      );
      setSubmitting(false);
    }
    // On success the action redirects to "/".
  };

  return (
    <form className={styles.form} onSubmit={submit}>
      <section className={styles.field}>
        <label className={styles.label} htmlFor="entry-name">
          Name
        </label>
        <input
          id="entry-name"
          className={styles.input}
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="Hen egg-white lysozyme"
          autoComplete="off"
        />
      </section>

      <section className={styles.field}>
        <label className={styles.label} htmlFor="entry-desc">
          Description
        </label>
        <textarea
          id="entry-desc"
          className={styles.textarea}
          value={description}
          onChange={(event) => setDescription(event.target.value)}
          placeholder="Short description of the protein"
          rows={3}
        />
      </section>

      <section className={styles.field}>
        <span className={styles.label}>Preview image</span>
        <label className={styles.thumbDrop}>
          {thumbPreview ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img className={styles.thumbImg} src={thumbPreview} alt="" />
          ) : (
            <span className={styles.thumbHint}>
              <UploadIcon />
              Choose an image
            </span>
          )}
          <input
            type="file"
            accept="image/*"
            className={styles.hiddenInput}
            onChange={onThumb}
          />
        </label>
      </section>

      <section className={styles.field}>
        <span className={styles.label}>Baseline data</span>
        <FilesEditor
          files={files}
          onAdd={addEntryFiles}
          onRemove={(id) => setFiles((p) => p.filter((f) => f.id !== id))}
          onLevel={(id, level) =>
            setFiles((p) => p.map((f) => (f.id === id ? { ...f, level } : f)))
          }
          onPatch={(id, patch) =>
            setFiles((p) => p.map((f) => (f.id === id ? { ...f, ...patch } : f)))
          }
        />
      </section>

      <section className={styles.field}>
        <div className={styles.expHead}>
          <span className={styles.label}>Models</span>
          <button type="button" className={styles.addExp} onClick={addExperiment}>
            <PlusIcon />
            Add model
          </button>
        </div>

        {experiments.length === 0 ? (
          <p className={styles.expEmpty}>No models yet.</p>
        ) : (
          <div className={styles.expList}>
            {experiments.map((exp, index) => (
              <div key={exp.id} className={styles.expCard}>
                <div className={styles.expCardHead}>
                  <span className={styles.expIndex}>#{index + 1}</span>
                  <button
                    type="button"
                    className={styles.remove}
                    onClick={() => removeExperiment(exp.id)}
                    aria-label="Remove model"
                  >
                    ×
                  </button>
                </div>
                <input
                  className={styles.input}
                  value={exp.name}
                  onChange={(event) =>
                    updateExperiment(exp.id, { name: event.target.value })
                  }
                  placeholder="Model name"
                  autoComplete="off"
                />
                <input
                  className={styles.input}
                  value={exp.description}
                  onChange={(event) =>
                    updateExperiment(exp.id, { description: event.target.value })
                  }
                  placeholder="Description (optional)"
                  autoComplete="off"
                />
                <label className={styles.thumbDrop}>
                  {exp.thumbPreview ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img className={styles.thumbImg} src={exp.thumbPreview} alt="" />
                  ) : (
                    <span className={styles.thumbHint}>
                      <UploadIcon />
                      Preview image
                    </span>
                  )}
                  <input
                    type="file"
                    accept="image/*"
                    className={styles.hiddenInput}
                    onChange={(event) => {
                      const file = event.target.files?.[0];
                      if (!file) {
                        return;
                      }
                      updateExperiment(exp.id, {
                        thumbFileId: crypto.randomUUID(),
                        thumbFile: file,
                        thumbPreview: URL.createObjectURL(file),
                      });
                    }}
                  />
                </label>
                <span className={styles.subLabel}>Data</span>
                <FilesEditor
                  files={exp.files}
                  onAdd={async (list) => {
                    const parsed = await Promise.all(
                      list.map((file) => parseFile(file, "L2")),
                    );
                    updateExperiment(exp.id, {
                      files: [...exp.files, ...parsed],
                    });
                  }}
                  onRemove={(id) =>
                    updateExperiment(exp.id, {
                      files: exp.files.filter((f) => f.id !== id),
                    })
                  }
                  onLevel={(id, level) =>
                    updateExperiment(exp.id, {
                      files: exp.files.map((f) =>
                        f.id === id ? { ...f, level } : f,
                      ),
                    })
                  }
                  onPatch={(id, patch) =>
                    updateExperiment(exp.id, {
                      files: exp.files.map((f) =>
                        f.id === id ? { ...f, ...patch } : f,
                      ),
                    })
                  }
                />

                <span className={styles.subLabel}>Metrics</span>
                <MetricsEditor
                  metrics={exp.metrics}
                  setMetrics={(next) =>
                    updateExperiment(exp.id, { metrics: next })
                  }
                />
              </div>
            ))}
          </div>
        )}
      </section>

      {error ? <p className={styles.error}>{error}</p> : null}

      <div className={styles.actions}>
        <a className={styles.secondary} href="/">
          Cancel
        </a>
        <button type="submit" className={styles.primary} disabled={!canSubmit}>
          {submitting ? "Creating…" : "Create entry"}
        </button>
      </div>
    </form>
  );
}

function FilesEditor({
  files,
  onAdd,
  onRemove,
  onLevel,
  onPatch,
}: {
  files: ParsedFile[];
  onAdd: (list: File[]) => void;
  onRemove: (id: string) => void;
  onLevel: (id: string, level: EntityLevel) => void;
  onPatch: (id: string, patch: Partial<ParsedFile>) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);

  const onChange = (event: ChangeEvent<HTMLInputElement>) => {
    const list = Array.from(event.target.files ?? []);
    event.target.value = "";
    if (list.length > 0) {
      onAdd(list);
    }
  };

  return (
    <div className={styles.filesEditor}>
      <button
        type="button"
        className={styles.fileDrop}
        onClick={() => inputRef.current?.click()}
      >
        <UploadIcon />
        Add data/model
        <span className={styles.fileDropHint}>FASTA, PDB, mmCIF, maps, logs…</span>
      </button>
      <input
        ref={inputRef}
        type="file"
        multiple
        className={styles.hiddenInput}
        onChange={onChange}
      />

      {files.length > 0 ? (
        <ul className={styles.fileList}>
          {files.map((file) => (
            <li key={file.id} className={styles.fileItem}>
              <div className={styles.fileRow}>
                <span className={styles.fileType}>{file.type}</span>
                <span className={styles.fileMeta}>
                  <span className={styles.fileName}>{file.name}</span>
                  <span className={styles.fileSub}>
                    {fileEntityType(file)}
                    {" · "}
                    {formatSize(file.size)}
                    {typeof file.metadata?.length === "number"
                      ? ` · ${file.metadata.length} residues`
                      : ""}
                  </span>
                </span>
                <select
                  className={styles.levelSelect}
                  value={file.level}
                  onChange={(event) =>
                    onLevel(file.id, event.target.value as EntityLevel)
                  }
                  aria-label="Level"
                >
                  {LEVELS.map((level) => (
                    <option key={level} value={level}>
                      {level}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  className={styles.remove}
                  onClick={() => onRemove(file.id)}
                  aria-label={`Remove ${file.name}`}
                >
                  ×
                </button>
              </div>

              <div className={styles.fileDepositorGrid}>
                <label className={styles.fileDepositorField}>
                  <span>Authors</span>
                  <textarea
                    className={styles.textarea}
                    value={file.authors}
                    onChange={(event) =>
                      onPatch(file.id, { authors: event.target.value })
                    }
                    placeholder={"Hendrickson, W.A.\nTeeter, M.M."}
                    rows={2}
                  />
                </label>
                <label className={styles.fileDepositorField}>
                  <span>Affiliation</span>
                  <input
                    className={styles.input}
                    value={file.affiliation}
                    onChange={(event) =>
                      onPatch(file.id, {
                        affiliation: event.target.value,
                      })
                    }
                    placeholder="Department of Chemistry, Boston University"
                    autoComplete="organization"
                  />
                </label>
              </div>

              {file.type === "image" && file.preview ? (
                <div className={styles.filePreview}>
                  {/* eslint-disable-next-line @next/next/no-img-element */}
                  <img className={styles.filePreviewImg} src={file.preview} alt="" />
                </div>
              ) : file.type === "fasta" && file.metadata ? (
                <div className={styles.filePreview}>
                  <SequenceView metadata={file.metadata as FastaMetadata} />
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function MetricsEditor({
  metrics,
  setMetrics,
}: {
  metrics: MetricDraft[];
  setMetrics: (next: MetricDraft[]) => void;
}) {
  const add = () =>
    setMetrics([...metrics, { id: crypto.randomUUID(), values: {} }]);
  const patch = (id: string, next: Partial<MetricDraft>) =>
    setMetrics(metrics.map((m) => (m.id === id ? { ...m, ...next } : m)));
  const remove = (id: string) =>
    setMetrics(metrics.filter((m) => m.id !== id));

  return (
    <div className={styles.filesEditor}>
      <button type="button" className={styles.fileDrop} onClick={add}>
        <PlusIcon />
        Add metrics
      </button>

      {metrics.map((metric) => (
        <div key={metric.id} className={styles.metricCard}>
          <div className={styles.metricTop}>
            <span className={styles.metricTag}>Metrics · L3</span>
            <button
              type="button"
              className={styles.remove}
              onClick={() => remove(metric.id)}
              aria-label="Remove metrics"
            >
              ×
            </button>
          </div>
          <div className={styles.metricGrid}>
            {METRIC_FIELDS.map((field) => (
              <label key={field.key} className={styles.metricField}>
                <span>{field.label}</span>
                <input
                  className={styles.input}
                  type="text"
                  inputMode="decimal"
                  value={metric.values[field.key] ?? ""}
                  onChange={(event) =>
                    patch(metric.id, {
                      values: {
                        ...metric.values,
                        [field.key]: sanitizeNumeric(event.target.value),
                      },
                    })
                  }
                  placeholder="—"
                />
              </label>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function toEntity(file: ParsedFile) {
  const entityType = fileEntityType(file);
  const payload: Record<string, unknown> = {
    file_url: file.url,
    size: file.size,
    ...(file.metadata ? { metadata: file.metadata } : {}),
  };
  if (entityType === "data") {
    payload.type = file.type;
  }
  const authors = authorsTextToList(file.authors);
  if (authors.length > 0) {
    payload.authors = authors;
  }
  const affiliation = file.affiliation.trim();
  if (affiliation) {
    payload.affiliation = affiliation;
  }

  return {
    id: file.id,
    type: entityType,
    level: file.level,
    name: file.name,
    payload,
  };
}

function fileEntityType(file: ParsedFile): "data" | "model" {
  return file.type === "pdb" || file.type === "mmcif" ? "model" : "data";
}

function authorsTextToList(text: string): string[] {
  return text
    .split(/\r?\n|;/)
    .map((author) => author.trim())
    .filter(Boolean);
}

// Restrict metric input to a single decimal number. Commas are treated as the
// decimal separator and normalized to a dot, so "0,205" and "0.205" both store
// as "0.205"; anything non-numeric is dropped as you type.
function sanitizeNumeric(raw: string): string {
  let s = raw.replace(/,/g, ".").replace(/[^0-9.\-]/g, "");
  const firstDot = s.indexOf(".");
  if (firstDot !== -1) {
    s = s.slice(0, firstDot + 1) + s.slice(firstDot + 1).replace(/\./g, "");
  }
  s = s.replace(/(?!^)-/g, "");
  return s;
}

function toMetricEntity(metric: MetricDraft) {
  const payload: Record<string, number> = {};
  for (const field of METRIC_FIELDS) {
    const raw = metric.values[field.key];
    const value = raw != null && raw.trim() !== "" ? Number(raw) : NaN;
    if (!Number.isNaN(value)) {
      payload[field.key] = value;
    }
  }
  return {
    id: metric.id,
    type: "metrics" as const,
    level: "L3" as const,
    name: "Metrics",
    payload,
  };
}

async function parseFile(file: File, level: EntityLevel): Promise<ParsedFile> {
  const type = detectType(file.name);
  const base: ParsedFile = {
    id: crypto.randomUUID(),
    file,
    name: file.name,
    size: file.size,
    type,
    level,
    authors: "",
    affiliation: "",
    url: "",
  };
  if (type === "fasta") {
    try {
      base.metadata = parseFasta(await file.text());
    } catch {
      // keep the file without metadata on parse failure
    }
  }
  if (type === "image") {
    base.preview = URL.createObjectURL(file);
  }
  return base;
}

type FileUploadLocation = Pick<FileUploadContext, "entryId" | "experimentId">;

async function uploadParsedFile(
  file: ParsedFile,
  location: FileUploadLocation,
): Promise<ParsedFile> {
  return {
    ...file,
    url: await uploadFileToObjectStorage(file.file, {
      ...location,
      entityId: file.id,
      filename: file.name,
    }),
  };
}

function detectType(filename: string): string {
  const ext = filename.split(".").pop()?.toLowerCase() ?? "";
  const map: Record<string, string> = {
    fasta: "fasta",
    fa: "fasta",
    faa: "fasta",
    seq: "fasta",
    pdb: "pdb",
    cif: "mmcif",
    mmcif: "mmcif",
    ccp4: "ccp4",
    mrc: "mrc",
    map: "ccp4",
    mtz: "mtz",
    h5: "hdf5",
    hdf5: "hdf5",
    cbf: "cbf",
    png: "image",
    jpg: "image",
    jpeg: "image",
    log: "log",
    txt: "text",
    json: "json",
    csv: "csv",
  };
  return map[ext] ?? ext ?? "file";
}

function parseFasta(text: string): Record<string, unknown> {
  const lines = text.split(/\r?\n/);
  let chains = 0;
  let sequence = "";
  for (const line of lines) {
    if (line.startsWith(">")) {
      chains += 1;
      continue;
    }
    sequence += line.trim();
  }
  sequence = sequence.replace(/\s+/g, "").toUpperCase();
  return { chains: chains || 1, length: sequence.length, sequence };
}

function formatSize(size: number): string {
  if (size <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = size;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const rounded = unit === 0 ? value : Math.round(value * 10) / 10;
  return `${rounded} ${units[unit]}`;
}

function UploadIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 16V4m0 0 4 4m-4-4-4 4M5 20h14" />
    </svg>
  );
}

function PlusIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}
