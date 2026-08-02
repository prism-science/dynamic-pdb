"use client";

import {
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type KeyboardEvent,
} from "react";

import type { EntityLevel, FastaMetadata } from "@/lib/api/entries";
import {
  extFileKey,
  searchExtFiles,
  type ExtExperiment,
  type ExtFile,
} from "@/lib/api/ext";
import { fastaTotalLength } from "@/lib/fasta";

import SequenceView from "../SequenceView";
import { LinkIcon, SearchIcon, UploadIcon } from "./icons";
import { LEVELS } from "./types";
import type { ParsedFile } from "./types";
import {
  detectType,
  droppedFiles,
  extFileKeys,
  fileEntityType,
  formatSize,
  hasDraggedFiles,
  isModelFile,
  isValidHttpUrl,
  uploadStatusText,
  authorsTextToList,
} from "./helpers";
import styles from "./form.module.css";

// Nothing rejects a sequence dropped on a model, but the hint should not
// suggest it either: a sequence belongs to the structure, not to one of its
// models. Callers that scope files to a model pass their own wording.
const DEFAULT_HINT = "Sequences, structures, maps — .fasta, .pdb, .cif, .ccp4, .log";

export default function FilesEditor({
  files,
  onAdd,
  onAddUrl,
  onAddExt,
  onRemove,
  onLevel,
  onPatch,
  extExperiment = null,
  lockLevel = false,
  lockModelLevel = false,
  hint = DEFAULT_HINT,
}: {
  files: ParsedFile[];
  onAdd: (list: File[]) => void;
  onAddUrl: (url: string) => void;
  onAddExt?: (files: ExtFile[]) => void;
  onRemove: (id: string) => void;
  onLevel: (id: string, level: EntityLevel) => void;
  onPatch: (id: string, patch: Partial<ParsedFile>) => void;
  extExperiment?: ExtExperiment | null;
  lockLevel?: boolean;
  lockModelLevel?: boolean;
  hint?: string;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [linkOpen, setLinkOpen] = useState(false);
  const [linkValue, setLinkValue] = useState("");
  const [extPickerOpen, setExtPickerOpen] = useState(false);
  const [dragging, setDragging] = useState(false);

  useEffect(() => {
    if (!extExperiment) {
      setExtPickerOpen(false);
    }
  }, [extExperiment]);

  // A file dropped just outside the zone would otherwise make the browser
  // navigate to it and take the half-filled form with it.
  useEffect(() => {
    const swallow = (event: globalThis.DragEvent) => {
      if (hasDraggedFiles(event.dataTransfer)) {
        event.preventDefault();
      }
    };
    window.addEventListener("dragover", swallow);
    window.addEventListener("drop", swallow);
    return () => {
      window.removeEventListener("dragover", swallow);
      window.removeEventListener("drop", swallow);
    };
  }, []);

  const onChange = (event: ChangeEvent<HTMLInputElement>) => {
    const list = Array.from(event.target.files ?? []);
    event.target.value = "";
    if (list.length > 0) {
      onAdd(list);
    }
  };

  const submitLink = () => {
    const url = linkValue.trim();
    if (!isValidHttpUrl(url)) {
      return;
    }
    onAddUrl(url);
    setLinkValue("");
    setLinkOpen(false);
  };

  return (
    <div className={styles.filesEditor}>
      <button
        type="button"
        className={styles.dropZone}
        data-dragging={dragging ? "true" : undefined}
        onClick={() => inputRef.current?.click()}
        onDragOver={(event) => {
          if (!hasDraggedFiles(event.dataTransfer)) {
            return;
          }
          event.preventDefault();
          setDragging(true);
        }}
        onDragLeave={(event) => {
          // Children of the zone fire dragleave too; ignore those so the
          // highlight doesn't flicker while moving across the label.
          const next = event.relatedTarget;
          if (next instanceof Node && event.currentTarget.contains(next)) {
            return;
          }
          setDragging(false);
        }}
        onDrop={(event) => {
          event.preventDefault();
          setDragging(false);
          const list = droppedFiles(event.dataTransfer);
          if (list.length > 0) {
            onAdd(list);
          }
        }}
      >
        <UploadIcon />
        <span className={styles.dropZoneTitle}>
          Drop files here or <span className={styles.dropZoneBrowse}>browse</span>
        </span>
        <span className={styles.dropZoneHint}>{hint}</span>
      </button>

      <div className={styles.altSourceRow}>
        <span className={styles.altSourceLabel}>or add from</span>
        <button
          type="button"
          className={styles.altSource}
          onClick={() => setLinkOpen((open) => !open)}
          aria-expanded={linkOpen}
          aria-label="Add a file from a URL"
        >
          <LinkIcon />
          URL
        </button>
        {extExperiment && onAddExt ? (
          <button
            type="button"
            className={styles.altSource}
            onClick={() => setExtPickerOpen(true)}
            aria-label="Add files from the linked Ext experiment"
          >
            <span className={styles.extTag}>Ext</span>
            experiment files
          </button>
        ) : null}
      </div>
      <input
        ref={inputRef}
        type="file"
        multiple
        className={styles.hiddenInput}
        onChange={onChange}
      />

      {linkOpen ? (
        <div className={styles.linkRow}>
          <input
            className={styles.input}
            type="url"
            inputMode="url"
            value={linkValue}
            onChange={(event) => setLinkValue(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                submitLink();
              }
            }}
            placeholder="e.g. https://example.org/structure.pdb"
            autoFocus
          />
          <button
            type="button"
            className={styles.linkAddBtn}
            onClick={submitLink}
            disabled={!isValidHttpUrl(linkValue.trim())}
          >
            Add
          </button>
        </div>
      ) : null}

      {extPickerOpen && extExperiment && onAddExt ? (
        <ExtFilePicker
          experiment={extExperiment}
          existingKeys={extFileKeys(files)}
          onAdd={(selected) => {
            onAddExt(selected);
            setExtPickerOpen(false);
          }}
          onClose={() => setExtPickerOpen(false)}
        />
      ) : null}

      {files.length > 0 ? (
        <ul className={styles.fileList}>
          {files.map((file) => (
            <li key={file.id} className={styles.fileItem}>
              {(() => {
                const modelLevelLocked = lockModelLevel && isModelFile(file);
                const levelLocked = lockLevel || modelLevelLocked;
                const lockedLevelTitle = modelLevelLocked
                  ? "Model files are always level L2"
                  : "Baseline data is always level L0";
                return file.uploadStatus === "uploading" ? (
                  <div className={styles.uploadingRow}>
                    <span className={styles.fileType}>{file.type}</span>
                    <div className={styles.uploadingMeta}>
                      <span className={styles.fileName}>{file.name}</span>
                      <ProgressBar value={file.progress} />
                    </div>
                    <button
                      type="button"
                      className={styles.remove}
                      onClick={() => onRemove(file.id)}
                      aria-label={`Cancel ${file.name}`}
                    >
                      ×
                    </button>
                  </div>
                ) : file.uploadStatus === "failed" ? (
                  <div className={styles.fileRow}>
                    <span className={styles.fileType} data-failed="true">
                      {file.type}
                    </span>
                    <span className={styles.fileMeta}>
                      <span className={styles.fileName}>{file.name}</span>
                      <span
                        className={styles.fileUploadStatus}
                        data-state="failed"
                      >
                        {uploadStatusText(
                          file.uploadStatus,
                          file.uploadError,
                        )}
                      </span>
                    </span>
                    <button
                      type="button"
                      className={styles.remove}
                      onClick={() => onRemove(file.id)}
                      aria-label={`Remove ${file.name}`}
                    >
                      ×
                    </button>
                  </div>
                ) : (
                  <>
                    <div className={styles.fileRow}>
                      <span className={styles.fileType}>{file.type}</span>
                      <span className={styles.fileMeta}>
                        <span className={styles.fileNameRow}>
                          <span className={styles.fileName}>{file.name}</span>
                          {file.source === "url" ? (
                            <span className={styles.sourceBadge}>
                              <LinkIcon />
                              link
                            </span>
                          ) : file.source === "ext" ? (
                            <span className={styles.sourceBadge}>Ext</span>
                          ) : null}
                        </span>
                        <span className={styles.fileSub}>
                          {fileEntityType(file)}
                          {file.size > 0 ? ` · ${formatSize(file.size)}` : ""}
                          {file.type === "fasta" && file.metadata
                            ? ` · ${fastaTotalLength(file.metadata as FastaMetadata)} residues`
                            : ""}
                        </span>
                      </span>
                      {levelLocked ? (
                        <span
                          className={styles.levelBadge}
                          data-level={modelLevelLocked ? "L2" : file.level}
                          title={lockedLevelTitle}
                        >
                          {modelLevelLocked ? "L2" : file.level}
                        </span>
                      ) : (
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
                      )}
                      <button
                        type="button"
                        className={styles.remove}
                        onClick={() => onRemove(file.id)}
                        aria-label={`Remove ${file.name}`}
                      >
                        ×
                      </button>
                    </div>

                    <div className={styles.depositBlock}>
                      <div className={styles.depositField}>
                        <span className={styles.depositLabel}>Authors</span>
                        <AuthorsInput
                          value={file.authors}
                          onChange={(next) =>
                            onPatch(file.id, { authors: next })
                          }
                        />
                      </div>
                      <div className={styles.depositField}>
                        <span className={styles.depositLabel}>Affiliation</span>
                        <input
                          className={styles.input}
                          value={file.affiliation}
                          onChange={(event) =>
                            onPatch(file.id, {
                              affiliation: event.target.value,
                            })
                          }
                          placeholder="e.g. Department of Chemistry, Boston University"
                          autoComplete="organization"
                        />
                      </div>
                    </div>

                    {file.type === "image" && file.preview ? (
                      <div className={styles.filePreview}>
                        {/* eslint-disable-next-line @next/next/no-img-element */}
                        <img
                          className={styles.filePreviewImg}
                          src={file.preview}
                          alt=""
                        />
                      </div>
                    ) : file.type === "fasta" && file.metadata ? (
                      <div className={styles.filePreview}>
                        <SequenceView metadata={file.metadata as FastaMetadata} />
                      </div>
                    ) : null}
                  </>
                );
              })()}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function ExtFilePicker({
  experiment,
  existingKeys,
  onAdd,
  onClose,
}: {
  experiment: ExtExperiment;
  existingKeys: Set<string>;
  onAdd: (files: ExtFile[]) => void;
  onClose: () => void;
}) {
  const [query, setQuery] = useState("");
  const [items, setItems] = useState<ExtFile[]>([]);
  const [nextCursor, setNextCursor] = useState<string | undefined>();
  const [selected, setSelected] = useState<Map<string, ExtFile>>(
    () => new Map(),
  );
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);
  const activeQuery = useRef(query);
  activeQuery.current = query;

  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      setLoading(true);
      setSearchError(null);
      void searchExtFiles(experiment.id, {
        query,
        signal: controller.signal,
      })
        .then((page) => {
          setItems(page.items);
          setNextCursor(page.next_cursor);
        })
        .catch((error) => {
          if (controller.signal.aborted) {
            return;
          }
          setItems([]);
          setNextCursor(undefined);
          setSearchError(
            error instanceof Error ? error.message : "Could not search files.",
          );
        })
        .finally(() => {
          if (!controller.signal.aborted) {
            setLoading(false);
          }
        });
    }, 250);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [experiment.id, query]);

  useEffect(() => {
    const closeOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [onClose]);

  const toggle = (file: ExtFile) => {
    const key = extFileKey(file);
    setSelected((current) => {
      const next = new Map(current);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.set(key, file);
      }
      return next;
    });
  };

  const loadMore = async () => {
    if (!nextCursor || loadingMore) {
      return;
    }
    setLoadingMore(true);
    setSearchError(null);
    const requestedQuery = query;
    try {
      const page = await searchExtFiles(experiment.id, {
        query: requestedQuery,
        cursor: nextCursor,
      });
      if (activeQuery.current !== requestedQuery) {
        return;
      }
      setItems((current) => [...current, ...page.items]);
      setNextCursor(page.next_cursor);
    } catch (error) {
      setSearchError(
        error instanceof Error ? error.message : "Could not load more files.",
      );
    } finally {
      setLoadingMore(false);
    }
  };

  return (
    <div
      className={styles.extPickerBackdrop}
      role="presentation"
      onMouseDown={(event) => {
        if (event.currentTarget === event.target) {
          onClose();
        }
      }}
    >
      <div
        className={styles.extPicker}
        role="dialog"
        aria-modal="true"
        aria-labelledby="ext-picker-title"
      >
        <div className={styles.extPickerHeader}>
          <span className={styles.extPickerTitleWrap}>
            <span className={styles.sourceMark}>Ext</span>
            <span>
              <strong id="ext-picker-title">Choose experiment files</strong>
              <span>{experiment.name}</span>
            </span>
          </span>
          <button
            type="button"
            className={styles.remove}
            onClick={onClose}
            aria-label="Close file picker"
          >
            ×
          </button>
        </div>

        <label className={styles.extSearch}>
          <SearchIcon />
          <input
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setNextCursor(undefined);
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
              }
            }}
            placeholder="Search by path, SHA-256, or label"
            autoFocus
          />
        </label>

        <div className={styles.extResults}>
          {loading ? (
            <p className={styles.extEmpty}>Searching…</p>
          ) : items.length === 0 ? (
            <p className={styles.extEmpty}>
              {searchError ?? "No matching files."}
            </p>
          ) : (
            <ul className={styles.extResultList}>
              {items.map((file) => {
                const key = extFileKey(file);
                const alreadyAdded = existingKeys.has(key);
                const disabled = alreadyAdded || !file.available;
                return (
                  <li key={key} className={styles.extResultItem}>
                    <label data-disabled={disabled ? "true" : undefined}>
                      <input
                        type="checkbox"
                        checked={selected.has(key) || alreadyAdded}
                        disabled={disabled}
                        onChange={() => toggle(file)}
                      />
                      <span className={styles.fileType}>
                        {detectType(file.name || file.path)}
                      </span>
                      <span className={styles.extResultMeta}>
                        <strong>
                          {file.name ||
                            file.path.split("/").filter(Boolean).pop() ||
                            "Ext file"}
                        </strong>
                        <span>{file.path}</span>
                      </span>
                      <span className={styles.extResultSide}>
                        {alreadyAdded
                          ? "Added"
                          : file.directory
                            ? "Directory"
                            : !file.available
                              ? file.error || "Unavailable"
                              : formatSize(file.size)}
                      </span>
                    </label>
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        <div className={styles.extPickerFooter}>
          <span>
            {selected.size === 0
              ? "No files selected"
              : `${selected.size} selected`}
          </span>
          <div className={styles.extPickerActions}>
            {nextCursor ? (
              <button
                type="button"
                className={styles.secondary}
                onClick={() => void loadMore()}
                disabled={loadingMore}
              >
                {loadingMore ? "Loading…" : "Load more"}
              </button>
            ) : null}
            <button
              type="button"
              className={styles.primary}
              disabled={selected.size === 0}
              onClick={() => onAdd(Array.from(selected.values()))}
            >
              Add selected
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

function AuthorsInput({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const [draft, setDraft] = useState("");
  const authors = authorsTextToList(value);

  const commit = (raw: string) => {
    const author = raw.trim();
    if (!author) {
      return;
    }
    onChange([...authors, author].join("\n"));
    setDraft("");
  };

  const removeAt = (index: number) => {
    onChange(authors.filter((_, i) => i !== index).join("\n"));
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter" || event.key === ",") {
      event.preventDefault();
      commit(draft);
    } else if (event.key === "Backspace" && draft === "" && authors.length > 0) {
      removeAt(authors.length - 1);
    }
  };

  return (
    <div className={styles.authorsInput}>
      {authors.map((author, index) => (
        <span key={`${author}-${index}`} className={styles.authorChip}>
          {author}
          <button
            type="button"
            className={styles.chipRemove}
            onClick={() => removeAt(index)}
            aria-label={`Remove ${author}`}
          >
            ×
          </button>
        </span>
      ))}
      <input
        className={styles.authorsField}
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={onKeyDown}
        onBlur={() => commit(draft)}
        placeholder={
          authors.length > 0
            ? "Add another…"
            : "e.g. Hendrickson, W.A. — press Enter"
        }
      />
    </div>
  );
}

export function ProgressBar({ value }: { value: number }) {
  const pct = Math.round(Math.min(1, Math.max(0, value)) * 100);
  return (
    <div className={styles.progressWrap}>
      <div className={styles.progressTrack}>
        <div className={styles.progressFill} style={{ width: `${pct}%` }} />
      </div>
      <span className={styles.progressPct}>{pct}%</span>
    </div>
  );
}
