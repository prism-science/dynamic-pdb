import { ApiRequestError, type ApiErrorObject } from "@/lib/api/entries";

import { FIELDS, SECTIONS, type FieldId } from "./fields";
import type { FieldMessage } from "./messages";

// Places the API's validation errors beside the field they are about. Each
// error carries a JSON pointer into the request body (for example
// /entry/polymer_entities/0/mutations/1); the field is the registry entry
// whose pointer pattern matches the most leading segments, and the item is
// read from the request itself, so it names the entity, crystal, metric or
// run the way the form does, whatever its position in the body.
//
// The banner above the form says, in words a depositor can act on, what went
// wrong when Dynamic PDB refused a request or never answered. It never shows a
// status code or a JSON pointer. This module is for the server actions: it
// reads ApiRequestError, which lives in a server-only module.

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
 * The title of the form section a pointer no field matches is most likely
 * about: the section of the field whose pointer shares the most leading
 * segments with it, beyond the request's top-level object. null when none does.
 */
export function sectionForPointer(pointer: string): string | null {
  const segments = pointerSegments(pointer);
  let best: { field: FieldId; shared: number } | null = null;
  for (const pattern of POINTER_PATTERNS) {
    const shared = sharedLeadingSegments(pattern.segments, segments);
    if (shared >= 2 && (!best || shared > best.shared)) {
      best = { field: pattern.field, shared };
    }
  }
  return best ? SECTIONS[FIELDS[best.field].section].title : null;
}

function sharedLeadingSegments(pattern: string[], segments: string[]): number {
  let shared = 0;
  while (
    shared < pattern.length &&
    shared < segments.length &&
    matchesPattern([pattern[shared]], [segments[shared]])
  ) {
    shared += 1;
  }
  return shared;
}

/**
 * Splits the API's errors into those a field of the form can show and those
 * it cannot. The second kind is kept as text for the banner, named by its
 * form section when one fits, never by its pointer.
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
      const section = pointer !== undefined ? sectionForPointer(pointer) : null;
      unplaced.push(section ? `${section}: ${errorText(error)}` : errorText(error));
      continue;
    }
    const { item, itemLabel } = itemForPointer(request, pointer);
    placed.push({ field, item, itemLabel, message: errorText(error), blocking: true });
  }
  return { placed, unplaced };
}

/** A request Dynamic PDB refused or never answered, as a server action saw it. */
export type RequestFailure = {
  /** The HTTP status, or null when Dynamic PDB never answered. */
  status: number | null;
  errors: readonly ApiErrorObject[];
  /** The error's own text, for the server log only. */
  detail: string;
};

/** The failure a server action caught. */
export function requestFailure(error: unknown): RequestFailure {
  if (error instanceof ApiRequestError) {
    return { status: error.status, errors: error.errors, detail: error.message };
  }
  return { status: null, errors: [], detail: error instanceof Error ? error.message : String(error) };
}

const DRAFT_KEPT = "Nothing you entered is lost";

/** The banner when some errors are shown beside their fields. */
function valuesMarkedBanner(unplaced: readonly string[]): string {
  const banner = "Dynamic PDB did not accept some values. They are marked below.";
  return unplaced.length > 0 ? `${banner} Also: ${unplaced.join("; ")}.` : banner;
}

/**
 * The banner for a failure: what went wrong and what to do next. placed says
 * whether some errors are shown beside their fields; unplaced are the rest,
 * as placeApiErrors words them.
 */
export function failureBanner(
  failure: RequestFailure,
  placed: boolean,
  unplaced: readonly string[],
): string {
  if (placed) {
    return valuesMarkedBanner(unplaced);
  }
  const said = unplaced.length > 0 ? ` ${unplaced.join("; ")}.` : "";
  if (failure.status === null) {
    // No answer, or an error that is not the API's: the server log keeps which.
    console.error(`no answer from the API: ${failure.detail}`);
    return `Dynamic PDB did not answer. ${DRAFT_KEPT}: try again in a minute.`;
  }
  switch (failure.status) {
    case 400:
    case 422:
      return `Dynamic PDB did not accept the request.${said}`;
    case 401:
      return `Your sign-in has expired. Sign in again in another tab, then submit again. ${DRAFT_KEPT}.`;
    case 403:
      return "Your account may not make this change. Ask a Dynamic PDB admin for access.";
    case 404:
      return "This entry or model is no longer there. Reload the page to see what is stored now.";
    case 409:
      return `This conflicts with what Dynamic PDB has stored:${said || " the entry changed since you opened it."} Reload the page and try again.`;
    case 413:
      return "The request is too large for Dynamic PDB. Remove the largest file and try again.";
    case 429:
      return `Dynamic PDB is busy. Wait a minute, then submit again. ${DRAFT_KEPT}.`;
    default:
      if (failure.status >= 500) {
        return `Dynamic PDB could not save this right now. ${DRAFT_KEPT}: try again in a minute, and tell the Dynamic PDB team if it keeps failing.`;
      }
      // A status the forms are not written for: the depositor gets a plain
      // message, and the server log keeps what Dynamic PDB said.
      console.error(`unexpected API status ${failure.status}: ${failure.detail}`);
      return `Dynamic PDB could not save this. ${DRAFT_KEPT}: try again, and tell the Dynamic PDB team if it keeps failing.`;
  }
}

/**
 * The result an action returns for a failed request. The request is rebuilt
 * only when the API sent errors to place: a request that failed to build has
 * none, and building it again would only throw again.
 */
export function apiFailure(failure: RequestFailure, request: () => unknown): ApiFailure {
  const { placed, unplaced } =
    failure.errors.length > 0
      ? placeApiErrors(failure.errors, request())
      : { placed: [], unplaced: [] };
  return {
    error: failureBanner(failure, placed.length > 0, unplaced),
    fieldErrors: placed,
  };
}
