import type { ApiErrorObject } from "@/lib/api/entries";

import { FIELDS, type FieldId } from "./fields";
import type { FieldMessage } from "./messages";

// Places the API's validation errors beside the field they are about. Each
// error carries a JSON pointer into the request body (for example
// /entry/polymer_entities/0/mutations/1); the field is the registry entry
// whose pointer pattern matches the most leading segments, and the item is
// read from the request itself, so it names the entity, crystal, metric or
// run the way the form does, whatever its position in the body.

/** What a create action returns when the API refuses the request. */
export type ApiFailure = {
  error: string;
  /** API errors the form can show beside a field. */
  fieldErrors: FieldMessage[];
};

type PointerPattern = { field: FieldId; segments: string[] };

const POINTER_PATTERNS: PointerPattern[] = (
  Object.keys(FIELDS) as FieldId[]
).flatMap((field) =>
  FIELDS[field].pointers.map((pointer) => ({
    field,
    segments: pointerSegments(pointer),
  })),
);

/** RFC 6901: "/" separates segments, "~1" is "/" and "~0" is "~". */
export function pointerSegments(pointer: string): string[] {
  if (pointer === "") {
    return [];
  }
  return pointer
    .split("/")
    .slice(1)
    .map((segment) => segment.replace(/~1/g, "/").replace(/~0/g, "~"));
}

function matchesPattern(pattern: string[], segments: string[]): boolean {
  return (
    pattern.length <= segments.length &&
    pattern.every((part, index) =>
      part === "*" ? /^[0-9]+$/.test(segments[index]) : part === segments[index],
    )
  );
}

export function fieldForPointer(pointer: string): FieldId | null {
  const segments = pointerSegments(pointer);
  let best: PointerPattern | null = null;
  for (const pattern of POINTER_PATTERNS) {
    if (
      matchesPattern(pattern.segments, segments) &&
      (!best || pattern.segments.length > best.segments.length)
    ) {
      best = pattern;
    }
  }
  return best?.field ?? null;
}

/** The ids of the array elements the pointer passes through, joined by "/".
 *  An element's id is its label_entity_id, key or id, in that order. */
export function itemForPointer(request: unknown, pointer: string): {
  item: string | null;
  itemLabel?: string;
} {
  const ids: string[] = [];
  let label: string | undefined;
  let node: unknown = request;
  for (const segment of pointerSegments(pointer)) {
    if (Array.isArray(node)) {
      node = node[Number(segment)];
      const record = asRecord(node);
      const id = record ? elementId(record) : null;
      if (id !== null) {
        ids.push(id);
        label = typeof record?.name === "string" ? record.name : label;
      }
    } else {
      const record = asRecord(node);
      if (!record) {
        break;
      }
      node = record[segment];
    }
  }
  return ids.length > 0 ? { item: ids.join("/"), itemLabel: label } : { item: null };
}

function elementId(record: Record<string, unknown>): string | null {
  for (const key of ["label_entity_id", "key", "id"]) {
    const value = record[key];
    if (typeof value === "string" && value !== "") {
      return value;
    }
  }
  return null;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function errorText(error: ApiErrorObject): string {
  return error.detail ?? error.title ?? error.code ?? "is not valid";
}

/**
 * Splits the API's errors into those a field of the form can show and those
 * it cannot. The second kind is kept as text for the banner.
 */
export function placeApiErrors(
  errors: readonly ApiErrorObject[],
  request: unknown,
): { placed: FieldMessage[]; unplaced: string[] } {
  const placed: FieldMessage[] = [];
  const unplaced: string[] = [];
  for (const error of errors) {
    const pointer = error.source?.pointer;
    const field = pointer !== undefined ? fieldForPointer(pointer) : null;
    if (pointer === undefined || field === null) {
      unplaced.push(pointer ? `${errorText(error)} (${pointer})` : errorText(error));
      continue;
    }
    const { item, itemLabel } = itemForPointer(request, pointer);
    placed.push({ field, item, itemLabel, message: errorText(error), blocking: true });
  }
  return { placed, unplaced };
}

/**
 * The result a create action returns for a refused request. The request is
 * rebuilt only when the API sent errors to place: a request that failed to
 * build has none, and building it again would only throw again.
 */
export function apiFailure(
  message: string,
  errors: readonly ApiErrorObject[],
  request: () => unknown,
): ApiFailure {
  if (errors.length === 0) {
    return { error: message, fieldErrors: [] };
  }
  const { placed, unplaced } = placeApiErrors(errors, request());
  if (placed.length === 0) {
    return { error: message, fieldErrors: [] };
  }
  const banner = "Dynamic PDB did not accept some values. They are marked below.";
  return {
    error: unplaced.length > 0 ? `${banner} Also: ${unplaced.join("; ")}` : banner,
    fieldErrors: placed,
  };
}
