#!/usr/bin/env python3
"""Download a saved Dynamic PDB selection file by file, with no size limit.

    python3 dynamic-pdb-download.py --selection <selection id> --files all_files

The selection ID is the one the download panel shows (or POST /v1/selections returns). The
script applies the archive's rules, so it gives the files a browser archive of the same
selection and file choice gives, at the same paths:

  original_mmcif        the PDB's coordinate file of each selected model whose record holds it
                        (usually the Deposited models)
  modified_coordinates  the coordinate files of the selected Dynamic PDB (computed) models
  all_files             every file of the selected models and their entries

Only files Dynamic PDB holds are downloaded: our uploads, and our copies of external files. Each
one is checked against its recorded SHA-256 and size and kept only when both match. Files
Dynamic PDB holds no copy of (for example over 1 GiB) are listed as linked, with the address to
fetch them from. Nothing is ever replaced by a newer file.

Files go to <out>/dynamic-pdb-<selection id>/, laid out as in the archive:
  <entry id>/<file>             files of the entry (for example its sequence)
  <entry id>/<model id>/<file>  files of the model, its runs' inputs and outputs
A file several selected models share is stored once. manifest.tsv lists every file of each
selected model with its status (included, linked or unavailable), its size and where that size
comes from (never estimated), whether it was verified against the bytes at save time, and its
reuse terms, in the archive manifest's columns. Two names that differ only in case or Unicode form get the archive's
ID prefix, so no file overwrites another on a filesystem that treats them as one name.

Run the script again after a break: files already saved with the right SHA-256 are kept, and
only the rest are downloaded. A file an earlier run saved is used again, never overwritten: if a
name clash resolves the other way (a file that failed before is saved now), whatever is in the way
moves to .displaced/ and is taken back from there. One folder holds one file choice: give each
choice its own --out. The script writes only inside the selection's folder and stops at a symbolic
link there.

Exit status: 0 when every file Dynamic PDB holds was saved and checked; 1 when one could not be
read or differs from its record (it is listed in manifest.tsv, never replaced); 2 when the
script could not run (bad arguments, unknown selection, the server could not be reached).

PDB archive files and Prism's own models are dedicated to the public domain under CC0 1.0. Files
deposited by others: Reuse terms not yet confirmed by the depositor. We ask that you acknowledge
Dynamic PDB and cite the depositors of the entries you use. Each file's terms are in manifest.tsv.

Needs Python 3.8 or newer and nothing else, on macOS or Linux. On Windows the script stops at once
(exit status 2): run it in WSL (Windows Subsystem for Linux), or download the archive from the
website's download panel.
"""

import argparse
import hashlib
import http.client
import json
import ntpath
import os
import re
import sys
import tempfile
import time
import unicodedata
import urllib.error
import urllib.parse
import urllib.request
import uuid
from datetime import datetime, timedelta, timezone

SCRIPT_VERSION = "1"
DEFAULT_API = "https://dynamicpdb.com/api"
# Where each Dynamic PDB deployment keeps files, by its API: the public files address and the
# bucket behind it (cdn in backend/config/production.yml and dev.yml). A file whose location is
# elsewhere is not ours, so the script never downloads it. Any other API needs --storage-base.
DEPLOYMENT_STORAGE_BASES = {
    "https://dynamicpdb.com/api": (
        "https://files.dynamicpdb.com",
        "https://dynamic-pdb-n-data.s3.us-west-1.amazonaws.com",
    ),
    "https://dev.dynamicpdb.com/api": (
        "https://dev-files.dynamicpdb.com",
        "https://dev-dynamic-pdb-n-data.s3.us-west-1.amazonaws.com",
    ),
}
FILE_CHOICES = ("original_mmcif", "modified_coordinates", "all_files")
PAGE_SIZE = 100
REQUEST_TIMEOUT_SECONDS = 60
API_ATTEMPTS = 5
FILE_READ_ATTEMPTS = 3
CHUNK_BYTES = 1024 * 1024
USER_AGENT = "dynamic-pdb-download/" + SCRIPT_VERSION

CC0_NAME = "CC0 1.0"
CC0_URL = "https://creativecommons.org/publicdomain/zero/1.0/"
# The reuse terms, word for word as the archive's README and manifest say them (researchers' Q7).
CC0_STATEMENT = "PDB archive files and Prism's own models are dedicated to the public domain under CC0 1.0."
NOT_CONFIRMED_TERMS = "Reuse terms not yet confirmed by the depositor"
EXTERNAL_TERMS = "See the repository's terms"
ACKNOWLEDGEMENT_REQUEST = "We ask that you acknowledge Dynamic PDB and cite the depositors of the entries you use."
# A file the archive cannot vouch for as it was when the selection was saved (researchers' Q10).
NOT_VERIFIED_AT_SAVE = "not verified against the bytes at save time"
PDB_ARCHIVE_HOSTS = ("files.rcsb.org", "www.rcsb.org")
COMPUTED_MODEL_TYPES = ("Single Conformer", "Multiconformer", "Ensemble")
NOT_COMPUTED_MODEL_TYPES = ("Deposited",)

MANIFEST_COLUMNS = (
    "record_type", "entry_id", "entry_revision", "model_id", "model_revision", "model_type", "pdb_id",
    "file_id", "file_name", "path_in_archive", "roles", "type", "level", "format", "size_bytes",
    "size_source", "recorded_sha256", "sha256_source", "recorded_after_save", "verified_at_save", "status",
    "reason", "reason_code", "verification", "verified_at", "source", "source_url", "license", "license_url",
    "authors", "affiliation", "citation_link", "dataset_id", "relation", "link_evidence",
)

# Statuses and verifications, as the archive's manifest writes them.
INCLUDED = "included"
LINKED = "linked"
UNAVAILABLE = "unavailable"
VERIFIED = "verified"
MISMATCH = "mismatch"
NOT_VERIFIED = "not_verified"

# The reason codes this script can give, with the archive's words for each. read_failed is worded
# for a script run: running it again is how the file is asked for again.
REASON_TEXTS = {
    "over_copy_limit": "larger than 1 GiB, so Dynamic PDB keeps only its link",
    "copy_failed": "Dynamic PDB could not keep a copy",
    "copy_off": "Dynamic PDB keeps no copy (copying was off)",
    "not_checked_yet": "not checked yet, so Dynamic PDB holds no copy yet",
    "stored_file_differs": "the file in Dynamic PDB's storage differs from its record",
    "original_bytes_unavailable": "original bytes unavailable: the source now serves other bytes, and no copy was kept",
    "source_unreadable": "the file cannot be read at its source",
    "copy_not_in_storage": "the location of Dynamic PDB's copy is not in its storage",
    "copy_missing": "Dynamic PDB's copy of the file is missing",
    "copy_differs": "Dynamic PDB's copy differs from the recorded SHA-256",
    "read_failed": "Dynamic PDB's copy could not be read; run the script again to fetch it",
}
# Reasons for a file the script tried to read itself: the run did not save a file Dynamic PDB holds.
READ_REASONS = ("copy_missing", "copy_differs", "read_failed")

NO_MATCHING_FILE_REASONS = {
    "original_mmcif": (
        "this model's record holds no original mmCIF: the PDB's coordinate file belongs to the "
        "entry's Deposited model, so select that model to include it"
    ),
    "modified_coordinates": "this model's record holds no Dynamic PDB coordinate file: it is a PDB archive model",
}

MAX_FILE_NAME_BYTES = 255
MAX_KEPT_EXTENSION_BYTES = 32


WINDOWS_MESSAGE = (
    "this script runs on macOS and Linux only. On Windows, run it in WSL (Windows Subsystem for "
    "Linux), or download the archive from the website's download panel."
)


class ScriptError(Exception):
    """A problem that stops the run: printed without a traceback, exit status 2."""


def unknown_value(kind, value):
    return ScriptError("unknown {}: {!r}; this script may be older than the server, download it again".format(kind, value))


# ---------------------------------------------------------------------------------------------
# The archive's rules, as backend/cmd/archive-worker/worker/files.go and manifest.go apply them.
# ---------------------------------------------------------------------------------------------


def go_clean(value):
    """Go's path.Clean for a rooted path: no empty or '.' parts, '..' resolved."""
    parts = []
    for part in value.split("/"):
        if part in ("", "."):
            continue
        if part == "..":
            if parts:
                parts.pop()
            continue
        parts.append(part)
    return "/" + "/".join(parts)


def clean_path_prefix(value):
    cleaned = go_clean("/" + (value[1:] if value.startswith("/") else value))
    if cleaned == "/":
        return ""
    return cleaned


def host_of(netloc):
    """The host and port of a URL's network location, without user info."""
    return netloc.rsplit("@", 1)[-1]


def parse_storage_base(value):
    parsed = urllib.parse.urlsplit(value.strip())
    if parsed.scheme not in ("http", "https") or not parsed.netloc:
        raise ScriptError("storage base must be an HTTP URL with a host: {!r}".format(value))
    if "@" in parsed.netloc or parsed.query or parsed.fragment:
        raise ScriptError("storage base must not have user info, a query or a fragment: {!r}".format(value))
    return (parsed.scheme.lower(), parsed.netloc.lower(), clean_path_prefix(urllib.parse.unquote(parsed.path)))


def storage_bases(api, given):
    """The storage bases: those given with --storage-base, else the deployment's of a known API."""
    if given:
        return [parse_storage_base(base) for base in given]
    deployment = DEPLOYMENT_STORAGE_BASES.get(api)
    if deployment is None:
        raise ScriptError("{} is not a Dynamic PDB deployment this script knows ({}); say where it keeps "
                          "files with --storage-base".format(api, ", ".join(DEPLOYMENT_STORAGE_BASES)))
    return [parse_storage_base(base) for base in deployment]


def in_storage(uri, bases):
    """Whether uri points at a file in Dynamic PDB's storage: under one of the bases, with a key."""
    try:
        parsed = urllib.parse.urlsplit(uri.strip())
    except ValueError:
        return False
    if not parsed.scheme or not parsed.netloc or "@" in parsed.netloc or parsed.query or parsed.fragment:
        return False
    path = clean_path_prefix(urllib.parse.unquote(parsed.path))
    for scheme, host, prefix in bases:
        if parsed.scheme.lower() != scheme or parsed.netloc.lower() != host:
            continue
        if prefix == "":
            key = path[1:]
        elif path.startswith(prefix + "/"):
            key = path[len(prefix) + 1:]
        else:
            continue
        if key:
            return True
    return False


def source_of(file, bases):
    uri = file["uri"]
    if uri is None:
        return "external"
    if in_storage(uri, bases):
        return "dynamic_pdb_upload"
    try:
        host = host_of(urllib.parse.urlsplit(uri).netloc).lower()
    except ValueError:
        return "external"
    if host in PDB_ARCHIVE_HOSTS:
        return "pdb_archive"
    return "external"


def is_pdb_coordinate_file(file):
    """The PDB's own coordinate file: a model file at files.rcsb.org/download/<PDB ID>.cif."""
    if file["type"] != "model" or file["uri"] is None:
        return False
    try:
        parsed = urllib.parse.urlsplit(file["uri"])
    except ValueError:
        return False
    if host_of(parsed.netloc).lower() != "files.rcsb.org":
        return False
    path = urllib.parse.unquote(parsed.path)
    if not path.startswith("/download/"):
        return False
    name = path[len("/download/"):].lower()
    return "/" not in name and name.endswith(".cif") and not name.endswith("-sf.cif")


def is_entry_file(file):
    """Whether the file belongs to the model only through its entry revision."""
    roles = file["roles"]
    return len(roles) > 0 and all(role == "entry" for role in roles)


def is_computed_model(model):
    """A Dynamic PDB model: Single Conformer, Multiconformer or Ensemble, without origin archive."""
    origin = model["origin"]
    if origin == "archive":
        return False
    if origin is not None:
        raise unknown_value("model origin", origin)
    model_type = model["model_type"]
    if model_type is None:
        return False
    if model_type in COMPUTED_MODEL_TYPES:
        return True
    if model_type in NOT_COMPUTED_MODEL_TYPES:
        return False
    raise unknown_value("model type", model_type)


def selects_file(choice, file, model):
    """Whether the file choice holds this file of the model."""
    if choice == "all_files":
        return True
    if choice == "original_mmcif":
        return not is_entry_file(file) and is_pdb_coordinate_file(file)
    if choice == "modified_coordinates":
        return (
            is_computed_model(model)
            and not is_entry_file(file)
            and file["type"] == "model"
            and not is_pdb_coordinate_file(file)
        )
    raise unknown_value("file choice", choice)


def state_of(file, bases):
    """How the archive lists a file before reading it: from the checksum job's last check.

    Returns (status, verification, verified_at, reason_code, location). location is set only for
    a file Dynamic PDB holds bytes of, which the script downloads.
    """
    checksum = file["checksum"]
    status = checksum["status"]
    copy_status = checksum["copy_status"]
    checked_at = checksum["checked_at"]
    if status == "verified":
        if copy_status is None:
            raise ScriptError("file {}: a verified result has no copy status".format(file["id"]))
        if copy_status == "own_storage":
            if file["uri"] is None:
                raise ScriptError("file {}: stored in our bucket without a URI".format(file["id"]))
            location = file["uri"]
        elif copy_status == "copied":
            if checksum["stored_uri"] is None:
                raise ScriptError("file {}: copied without the copy's URI".format(file["id"]))
            location = checksum["stored_uri"]
        elif copy_status == "over_limit":
            return (LINKED, VERIFIED, checked_at, "over_copy_limit", None)
        elif copy_status == "copy_failed":
            return (LINKED, VERIFIED, checked_at, "copy_failed", None)
        elif copy_status == "copy_off":
            return (LINKED, VERIFIED, checked_at, "copy_off", None)
        elif copy_status == "mismatch":
            raise ScriptError("file {}: a verified result with copy status mismatch".format(file["id"]))
        else:
            raise unknown_value("copy status", copy_status)
        if file["recorded_sha256"] is None or checksum["size_bytes"] is None:
            raise ScriptError("file {}: verified without a recorded SHA-256 or size".format(file["id"]))
        if not in_storage(location, bases):
            return (UNAVAILABLE, NOT_VERIFIED, None, "copy_not_in_storage", None)
        return (INCLUDED, VERIFIED, None, "", location)
    if status == "mismatch":
        if copy_status is None:
            raise ScriptError("file {}: a mismatch result has no copy status".format(file["id"]))
        if copy_status == "own_storage":
            return (UNAVAILABLE, MISMATCH, checked_at, "stored_file_differs", None)
        if copy_status == "mismatch":
            # The original bytes are gone: unavailable, never replaced (researchers' item 14).
            return (UNAVAILABLE, MISMATCH, checked_at, "original_bytes_unavailable", None)
        if copy_status in ("copied", "over_limit", "copy_failed", "copy_off"):
            raise ScriptError("file {}: a mismatch result with copy status {}".format(file["id"], copy_status))
        raise unknown_value("copy status", copy_status)
    if status == "pending":
        return (LINKED, NOT_VERIFIED, None, "not_checked_yet", None)
    if status == "failed":
        return (UNAVAILABLE, NOT_VERIFIED, checked_at, "source_unreadable", None)
    raise unknown_value("checksum status", status)


_TIME = re.compile(r"^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(\.\d+)?(Z|[+-]\d\d:\d\d)$")


def rfc3339_seconds(value):
    """An API time as the manifest writes it: UTC, whole seconds (Go's time.RFC3339)."""
    if value is None:
        return ""
    match = _TIME.match(value)
    if match is None:
        raise ScriptError("unreadable time from the server: {!r}".format(value))
    moment = datetime.strptime(match.group(1), "%Y-%m-%dT%H:%M:%S")
    zone = match.group(3)
    if zone != "Z":
        sign = 1 if zone[0] == "+" else -1
        moment -= sign * timedelta(hours=int(zone[1:3]), minutes=int(zone[4:6]))
    return moment.strftime("%Y-%m-%dT%H:%M:%SZ")


def now_rfc3339():
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def go_base(value):
    """Go's path.Base."""
    if value == "":
        return "."
    value = value.rstrip("/")
    if value == "":
        return "/"
    return value.rsplit("/", 1)[-1]


def safe_file_name(name, file_id):
    """The file's name kept inside its folder: its last path element, without control characters."""
    cleaned = "".join(char for char in name.replace("\\", "/") if ord(char) >= 32 and ord(char) != 127)
    cleaned = go_base(cleaned.strip()).strip()
    if cleaned in ("", ".", "..", "/"):
        return file_id
    return cleaned


def go_ext(name):
    """Go's path.Ext for a name without slashes: from the last dot, or empty."""
    index = name.rfind(".")
    return "" if index < 0 else name[index:]


def cut_utf8(value, limit):
    """At most limit bytes of value (bytes), ending at a character boundary."""
    if len(value) <= limit:
        return value
    while limit > 0 and (value[limit] & 0xC0) == 0x80:
        limit -= 1
    return value[:limit]


def bounded_file_name(prefix, name):
    """prefix + name within 255 bytes: a longer name loses the end of its stem and keeps its extension."""
    prefix_bytes = prefix.encode("utf-8")
    name_bytes = name.encode("utf-8")
    room = MAX_FILE_NAME_BYTES - len(prefix_bytes)
    if len(name_bytes) <= room:
        return prefix + name
    extension = go_ext(name).encode("utf-8")
    if len(extension) > MAX_KEPT_EXTENSION_BYTES or len(extension) == len(name_bytes):
        extension = b""
    stem = name_bytes[: len(name_bytes) - len(extension)]
    return (prefix_bytes + cut_utf8(stem, room - len(extension)) + extension).decode("utf-8")


def has_drive(name):
    """Whether Windows reads the name as on a drive ("C:name"), which would leave its folder."""
    return ntpath.splitdrive(name)[0] != ""


def check_path_part(kind, value):
    """An entry or model ID used as a folder name: refuse one that could leave the output folder."""
    if (value in ("", ".", "..") or "/" in value or "\\" in value or has_drive(value)
            or any(ord(char) < 32 for char in value)):
        raise ScriptError("the server sent {} {!r}, which cannot be a folder name".format(kind, value))
    return value


def same_name_key(path):
    """The key two paths share when a filesystem may treat them as one name: ignoring case
    (macOS, Windows) and Unicode normalization (macOS: "café" typed two ways)."""
    return unicodedata.normalize("NFD", unicodedata.normalize("NFD", path).casefold())


class Layout:
    """Each saved file's one path, as the archive gives it: entry files in the entry's folder, model
    files in the folder of the first model that lists them, and the file ID as a prefix when the
    name is taken. Names are compared ignoring case."""

    def __init__(self, selection_id):
        self.root = "dynamic-pdb-" + selection_id
        self.paths = {}
        self.used = {}
        self.folders = set()
        # taken(path) reports a path the disk already uses for a file of this run under another
        # name (an alias the name rules above do not know, such as "model.cif." on Windows).
        self.taken = None

    def model_folder(self, model):
        return "/".join((self.root, check_path_part("entry ID", model["entry_id"]),
                         check_path_part("model ID", model["model_id"])))

    def reserve_folder(self, folder):
        self.folders.add(same_name_key(folder))

    def path(self, file, model, entry_file, claim=True):
        """The file's path. With claim False, the path it would get now, without taking it."""
        assigned = self.paths.get(file["id"])
        if assigned is not None:
            return assigned
        if entry_file:
            folder = self.root + "/" + check_path_part("entry ID", model["entry_id"])
        else:
            folder = self.model_folder(model)
        name = safe_file_name(file["name"], file["id"])
        if has_drive(name):
            # "C:name" would leave the folder on Windows; the archive's zip keeps it as is.
            name = name.replace(":", "_")
        file_id = file["id"]
        for prefix in ("", file_id[:8] + "_", file_id + "_"):
            candidate = folder + "/" + bounded_file_name(prefix, name)
            if self._free(candidate, file_id):
                return self._take(candidate, file_id) if claim else candidate
        number = 2
        while True:
            candidate = folder + "/" + bounded_file_name("{}-{}_".format(file_id, number), name)
            if self._free(candidate, file_id):
                return self._take(candidate, file_id) if claim else candidate
            number += 1

    def _free(self, candidate, file_id):
        folded = same_name_key(candidate)
        owner = self.used.get(folded)
        if (owner is not None and owner != file_id) or folded in self.folders:
            return False
        return self.taken is None or not self.taken(candidate)

    def _take(self, candidate, file_id):
        self.paths[file_id] = candidate
        self.used[same_name_key(candidate)] = file_id
        return candidate


def size_values(file):
    """A file's size and where it comes from, never estimated (researchers' Q9), as the archive
    says them: read from bytes that matched the record, else declared when the file was added, else
    reported by its source; empty when none is known."""
    checksum = file["checksum"]
    if checksum["status"] == "verified" and checksum["size_bytes"] is not None:
        return str(checksum["size_bytes"]), "read"
    if file["size_bytes"] is not None:
        return str(file["size_bytes"]), "declared"
    if file["source_size_bytes"] is not None:
        return str(file["source_size_bytes"]), "source"
    return "", ""


def verified_at_save(file):
    """'no' when the archive cannot vouch for the bytes as they were when the selection was saved:
    the SHA-256 was recorded after the save, or is not recorded (researchers' Q10)."""
    source = file["recorded_sha256_source"]
    if source in ("declared", "computed"):
        return "no" if file["recorded_after_save"] else "yes"
    if source == "not_recorded":
        return "no"
    raise unknown_value("sha256 source", source)


def license_values(file):
    """The file's reuse terms as the archive's manifest writes them (researchers' Q7). The API says
    which apply, by the archive worker's own rule (who deposited the file, and where it is held)."""
    terms = file["reuse_terms"]
    if terms == "cc0":
        return CC0_NAME, CC0_URL
    if terms == "not_confirmed":
        return NOT_CONFIRMED_TERMS, ""
    if terms == "repository":
        return EXTERNAL_TERMS, file["uri"] or ""
    raise unknown_value("reuse terms", terms)


def record_values(model, with_model):
    row = {"entry_id": model["entry_id"], "entry_revision": str(model["entry_revision_number"])}
    if with_model:
        row["model_id"] = model["model_id"]
        row["model_revision"] = str(model["model_revision_number"])
        row["model_type"] = model["model_type"] or ""
    return row


def file_row(model, file, entry_file, outcome, bases):
    """A file's manifest row for the model it is listed with."""
    status, verification, verified_at, reason_code, path = outcome
    source = source_of(file, bases)
    license_name, license_url = license_values(file)
    size, size_source = size_values(file)
    row = record_values(model, not entry_file)
    row.update({
        "record_type": "file",
        "file_id": file["id"],
        "file_name": file["name"],
        "path_in_archive": path,
        "roles": ",".join(file["roles"]),
        "type": file["type"],
        "level": file["level"],
        "format": file["format"] or "",
        "size_bytes": size,
        "size_source": size_source,
        "recorded_sha256": file["recorded_sha256"] or "",
        "sha256_source": file["recorded_sha256_source"],
        "recorded_after_save": "true" if file["recorded_after_save"] else "false",
        "verified_at_save": verified_at_save(file),
        "status": status,
        "reason": REASON_TEXTS[reason_code] if reason_code else "",
        "reason_code": reason_code,
        "verification": verification,
        "verified_at": verified_at,
        "source": source,
        "source_url": file["uri"] or "",
        "license": license_name,
        "license_url": license_url,
    })
    return row


class Walk:
    """The selection's manifest rows, model by model, as the archive lists them: a row per file of
    the file choice (an entry file once per entry), and a row for a model with no such file.

    read(file, location, path_of) is called once per file Dynamic PDB holds and returns its
    outcome. path_of(claim) is the file's layout path; read takes it (claim True) only for a file it
    saves, so a file left out never holds a path another file could have, as in the archive."""

    def __init__(self, choice, layout, bases, read):
        if choice not in FILE_CHOICES:
            raise unknown_value("file choice", choice)
        self.choice = choice
        self.layout = layout
        self.bases = bases
        self.read = read
        self.entry_files = set()
        self.outcomes = {}

    def rows(self, model):
        rows = []
        matched = False
        for file in model["files"]:
            if not selects_file(self.choice, file, model):
                continue
            matched = True
            entry_file = is_entry_file(file)
            if entry_file:
                key = (model["entry_id"], file["id"])
                if key in self.entry_files:
                    continue
                self.entry_files.add(key)
            rows.append(file_row(model, file, entry_file, self._outcome(model, file, entry_file), self.bases))
        if not matched and self.choice != "all_files":
            row = record_values(model, True)
            row.update({"record_type": "no_matching_file", "reason": NO_MATCHING_FILE_REASONS[self.choice]})
            rows.append(row)
        return rows

    def _outcome(self, model, file, entry_file):
        status, verification, checked_at, reason_code, location = state_of(file, self.bases)
        if location is None:
            return (status, verification, rfc3339_seconds(checked_at), reason_code, "")
        done = self.outcomes.get(file["id"])
        if done is None:
            done = self.read(file, location, lambda claim: self.layout.path(file, model, entry_file, claim))
            self.outcomes[file["id"]] = done
        return done


def escape_value(value):
    return value.replace("\\", "\\\\").replace("\t", "\\t").replace("\n", "\\n").replace("\r", "\\r")


def manifest_line(row):
    for key in row:
        if key not in MANIFEST_COLUMNS:
            raise ScriptError("manifest row has unknown column {!r}".format(key))
    return "\t".join(escape_value(row.get(column, "")) for column in MANIFEST_COLUMNS) + "\n"


def manifest_header(selection_id, catalog_date, choice, started_at):
    return [
        "# Dynamic PDB download manifest, written by dynamic-pdb-download.py {}. Selection {}, catalog date {}, "
        "file choice {}, started {}.".format(SCRIPT_VERSION, selection_id, catalog_date, choice, started_at),
        "# {} {}: {}.".format(CC0_STATEMENT, CC0_NAME, CC0_URL),
        "# Files deposited by others: {}, until they confirm. Related datasets and files held elsewhere: {}. "
        "Each file's license column says which applies.".format(NOT_CONFIRMED_TERMS, EXTERNAL_TERMS),
        "# " + ACKNOWLEDGEMENT_REQUEST,
        "# path_in_archive is the file's path under the output folder, as in the archive. pdb_id, authors, "
        "affiliation and citation_link are empty and related datasets are not listed: the archive's "
        "manifest and each entry's page have them.",
        "\t".join(MANIFEST_COLUMNS),
    ]


# ---------------------------------------------------------------------------------------------
# Talking to the server and saving files.
# ---------------------------------------------------------------------------------------------


def say(message):
    print(message, file=sys.stderr, flush=True)


def problem_of(error):
    """The API's error code and detail from an HTTP error, when its body has them."""
    try:
        body = json.loads(error.read().decode("utf-8"))
        first = body["errors"][0]
        return first.get("code") or "", first.get("detail") or ""
    except (ValueError, KeyError, IndexError, TypeError, AttributeError, OSError, http.client.HTTPException):
        # An error body that is not the API's, or ends early: the status alone says enough.
        return "", ""


def retry_delay(error, attempt):
    if isinstance(error, urllib.error.HTTPError):
        header = error.headers.get("Retry-After") if error.headers is not None else None
        if header is not None and header.strip().isdigit():
            return min(int(header.strip()), 300)
    return min(2 ** attempt, 30)


class ApiError(ScriptError):
    """A 4xx answer from the API, with its error code."""

    def __init__(self, status, code, detail, url):
        super().__init__("the server answered {} {} for {}: {}".format(status, code, url, detail))
        self.status = status
        self.code = code


def get_json(url):
    """GET a JSON document from the API, waiting and asking again on 429, 5xx and network errors."""
    request = urllib.request.Request(url, headers={"Accept": "application/vnd.api+json", "User-Agent": USER_AGENT})
    for attempt in range(1, API_ATTEMPTS + 1):
        try:
            with urllib.request.urlopen(request, timeout=REQUEST_TIMEOUT_SECONDS) as response:
                return json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as error:
            if error.code == 429 or error.code >= 500:
                if attempt == API_ATTEMPTS:
                    raise ScriptError("the server answered {} for {} {} times".format(error.code, url, attempt))
                delay = retry_delay(error, attempt)
                say("The server answered {}; asking again in {} s.".format(error.code, delay))
                time.sleep(delay)
                continue
            code, detail = problem_of(error)
            raise ApiError(error.code, code, detail, url)
        except (urllib.error.URLError, http.client.HTTPException, OSError, ValueError) as error:
            # HTTPException: the answer ended early (IncompleteRead) or was malformed.
            if attempt == API_ATTEMPTS:
                raise ScriptError("could not read {}: {!r}".format(url, error))
            delay = retry_delay(error, attempt)
            say("Could not reach the server ({}); asking again in {} s.".format(error, delay))
            time.sleep(delay)
    raise ScriptError("could not read {}".format(url))


def read_selection(api, selection_id):
    try:
        document = get_json("{}/v1/selections/{}".format(api, selection_id))
    except ApiError as error:
        if error.status == 404 and error.code == "SELECTIONS_OFF":
            raise ScriptError("bulk download is not available on this server yet")
        if error.status == 404:
            raise ScriptError("no saved selection has the ID {}".format(selection_id))
        raise
    return document["data"]["attributes"]


def list_models(api, selection_id, sink):
    """Every model of the selection, page by page, written to sink as JSON lines. Returns the count."""
    count = 0
    cursor = None
    while True:
        query = {"limit": str(PAGE_SIZE)}
        if cursor is not None:
            query["cursor"] = cursor
        page = get_json("{}/v1/selections/{}/models?{}".format(api, selection_id, urllib.parse.urlencode(query)))
        for item in page["data"]:
            sink.write(json.dumps(item["attributes"]) + "\n")
            count += 1
        cursor = (page.get("meta") or {}).get("next_cursor")
        if not cursor or not page["data"]:
            return count
        say("Listed {:,} models.".format(count))


def sha256_of(path):
    digest = hashlib.sha256()
    size = 0
    with open(path, "rb") as stream:
        for chunk in iter(lambda: stream.read(CHUNK_BYTES), b""):
            digest.update(chunk)
            size += len(chunk)
    return digest.hexdigest(), size


SHA256_NAME = re.compile(r"^[0-9a-f]{64}$")


class Saver:
    """Saves the files Dynamic PDB holds in the selection's folder, each checked against its record
    before it takes its path, and writes nothing outside that folder.

    Files an earlier run of the same file choice saved are used again, never overwritten. Paths
    are the same from run to run, except when a file that failed before is saved now and takes a
    name another file had: whatever is at a path that is now another file's moves to .displaced/,
    named by its SHA-256, and its own file takes it back from there instead of downloading it."""

    MARK = ".dynamic-pdb-download.json"

    def __init__(self, out, root):
        self.out = out
        self.root = root
        self.displaced = os.path.join(root, ".displaced")
        self.saved = 0
        self.kept = 0
        # The SHA-256s of the files saved in this run, so redundant displaced copies can go.
        self.included = set()
        # The disk identities (device, inode) of the files saved in this run: a path that leads to
        # one of them under another name is never written over.
        self.placed = set()

    def local_path(self, relative):
        """The file's place on disk, which must be inside the selection's folder."""
        parts = relative.split("/")
        target = os.path.join(self.out, *parts)
        inside = os.path.abspath(self.root) + os.sep
        if len(parts) < 2 or not os.path.abspath(target).startswith(inside):
            raise ScriptError("{} would be outside {}".format(relative, self.root))
        return target

    def start(self, selection_id, choice):
        """Checks the folder holds no other download, marks it as this one's, and removes partial
        downloads an interrupted run left behind."""
        for held in self._held_choices():
            if held != (selection_id, choice):
                raise ScriptError("{} holds a download of selection {} with file choice {}; give file choice {} its "
                                  "own --out".format(self.root, held[0], held[1], choice))
        with open_new(os.path.join(self.root, self.MARK), "w", encoding="utf-8") as mark:
            json.dump({"selection": selection_id, "files": choice}, mark)
        for name in os.listdir(self.root):
            if name.startswith(".incoming-") and name.endswith(".part"):
                remove_quietly(os.path.join(self.root, name))

    def _held_choices(self):
        """Each (selection, file choice) the folder already records: the script's mark, and a
        manifest's first line (an unpacked archive's too). Empty for a new folder."""
        held = []
        mark = os.path.join(self.root, self.MARK)
        if os.path.lexists(mark):
            try:
                if os.path.islink(mark):
                    raise ValueError("a symbolic link")
                with open(mark, encoding="utf-8") as stream:
                    recorded = json.load(stream)
                held.append((recorded["selection"], recorded["files"]))
            except (OSError, ValueError, KeyError, TypeError) as error:
                raise ScriptError("{} is unreadable ({}); delete it if the folder is yours".format(mark, error))
        manifest = os.path.join(self.root, "manifest.tsv")
        if os.path.lexists(manifest):
            if os.path.islink(manifest) or not os.path.isfile(manifest):
                raise ScriptError("{} is not a file; give this download its own --out".format(manifest))
            with open(manifest, encoding="utf-8", errors="replace") as stream:
                first = stream.readline()
            found = re.search(r"Selection ([0-9a-f-]{36}), .*file choice ([a-z_]+)", first)
            if found is None:
                raise ScriptError("{} is not a Dynamic PDB manifest; give this download its own --out".format(manifest))
            held.append((found.group(1), found.group(2)))
        return held

    def _folder(self, parts, create):
        """Whether the folder of these parts below the selection's folder exists, creating it when
        asked. A symbolic link, a junction or any other folder that resolves elsewhere would lead
        outside, so it stops the run."""
        current = self.root
        real_parent = os.path.realpath(self.root)
        for part in parts:
            current = os.path.join(current, part)
            if os.path.islink(current):
                raise ScriptError("{} is a symbolic link; the script writes only inside {}".format(current, self.root))
            if os.path.isdir(current):
                real = os.path.realpath(current)
                if same_name_key(real) != same_name_key(os.path.join(real_parent, part)):
                    raise ScriptError("{} leads to {} (a link or junction); the script writes only inside {}".format(
                        current, real, self.root))
                real_parent = real
                continue
            if os.path.lexists(current):
                raise ScriptError("{} is in the way of a folder the script needs; move it away and run "
                                  "again".format(current))
            if not create:
                return False
            os.mkdir(current)
            real_parent = os.path.join(real_parent, part)
        return True

    @staticmethod
    def _holds(path, recorded, size):
        return not os.path.islink(path) and os.path.isfile(path) and sha256_of(path) == (recorded, size)

    def _displace(self, path):
        """Moves whatever is at path to .displaced/: a file by its SHA-256, anything else by a new
        name. Nothing is deleted unless .displaced/ already holds the very same bytes."""
        self._folder([".displaced"], create=True)
        if os.path.islink(path) or not os.path.isfile(path):
            os.replace(path, os.path.join(self.displaced, "other-" + uuid.uuid4().hex))
            return
        digest, size = sha256_of(path)
        target = os.path.join(self.displaced, digest)
        if os.path.lexists(target):
            if self._holds(target, digest, size):
                os.remove(path)  # The same bytes are already kept there.
                return
            os.replace(target, os.path.join(self.displaced, "other-" + uuid.uuid4().hex))
        os.replace(path, target)

    def _stashed(self, recorded, size):
        """A displaced copy of these bytes, or None."""
        if not SHA256_NAME.match(recorded) or not self._folder([".displaced"], create=False):
            return None
        path = os.path.join(self.displaced, recorded)
        return path if self._holds(path, recorded, size) else None

    def taken(self, relative):
        """Whether the path cannot hold a file: it leads to a folder (for example a model folder
        under another name), or to a file this run saved under another name."""
        target = self.local_path(relative)
        if not os.path.lexists(target):
            return False
        return os.path.isdir(target) or identity(target) in self.placed

    def _place(self, source, relative):
        parts = relative.split("/")
        self._folder(parts[1:-1], create=True)
        target = self.local_path(relative)
        if os.path.lexists(target):
            if identity(target) in self.placed:
                raise ScriptError("{} is a file this run already saved under another name".format(target))
            self._displace(target)
        os.replace(source, target)

    def read(self, file, location, path_of):
        recorded = (file["recorded_sha256"] or "").strip().lower()
        expected_size = file["checksum"]["size_bytes"]
        relative = path_of(False)
        present = self.local_path(relative)
        if self._folder(relative.split("/")[1:-1], create=False) and os.path.lexists(present):
            if self._holds(present, recorded, expected_size):
                return self._included(path_of(True), recorded, kept=True)
            self._displace(present)
        stashed = self._stashed(recorded, expected_size)
        if stashed is not None:
            relative = path_of(True)
            self._place(stashed, relative)
            return self._included(relative, recorded, kept=True)
        # Fetched next to the files, so the move to its path stays on one filesystem.
        partial = os.path.join(self.root, ".incoming-{}.part".format(file["id"]))
        failure = ""
        for attempt in range(1, FILE_READ_ATTEMPTS + 1):
            delay = min(2 ** attempt, 30)
            try:
                digest, size = self._fetch(location, partial)
            except urllib.error.HTTPError as error:
                remove_quietly(partial)
                if error.code == 404:
                    say("Missing: {} ({})".format(file["name"], location))
                    return (UNAVAILABLE, NOT_VERIFIED, "", "copy_missing", "")
                failure = "HTTP {}".format(error.code)
                delay = retry_delay(error, attempt)
            except (urllib.error.URLError, http.client.HTTPException, OSError) as error:
                remove_quietly(partial)
                failure = str(error) or type(error).__name__
            else:
                if digest != recorded or size != expected_size:
                    remove_quietly(partial)
                    say("Differs from its record, not saved: {} ({})".format(file["name"], location))
                    return (UNAVAILABLE, MISMATCH, "", "copy_differs", "")
                relative = path_of(True)
                self._place(partial, relative)
                say("Saved {} ({:,} bytes)".format(relative, size))
                return self._included(relative, recorded, kept=False)
            if attempt < FILE_READ_ATTEMPTS:
                time.sleep(delay)
        say("Could not read {} ({}): {}".format(file["name"], location, failure))
        return (UNAVAILABLE, NOT_VERIFIED, "", "read_failed", "")

    def _included(self, relative, recorded, kept):
        if kept:
            self.kept += 1
        else:
            self.saved += 1
        self.included.add(recorded)
        self.placed.add(identity(self.local_path(relative)))
        return (INCLUDED, VERIFIED, now_rfc3339(), "", relative)

    def finish(self):
        """Drops displaced copies of files this run saved; returns how many other ones are left."""
        if not os.path.isdir(self.displaced) or os.path.islink(self.displaced):
            return 0
        left = 0
        for name in os.listdir(self.displaced):
            path = os.path.join(self.displaced, name)
            redundant = (name in self.included and not os.path.islink(path) and os.path.isfile(path)
                         and sha256_of(path)[0] == name)
            if redundant:
                os.remove(path)
            else:
                left += 1
        if left == 0:
            os.rmdir(self.displaced)
        return left

    @staticmethod
    def _fetch(location, partial):
        request = urllib.request.Request(location, headers={"User-Agent": USER_AGENT})
        digest = hashlib.sha256()
        size = 0
        with urllib.request.urlopen(request, timeout=REQUEST_TIMEOUT_SECONDS) as response, \
                open_new(partial, "wb") as out:
            declared = (response.getheader("Content-Length") or "").strip()
            for chunk in iter(lambda: response.read(CHUNK_BYTES), b""):
                digest.update(chunk)
                size += len(chunk)
                out.write(chunk)
        if declared.isdigit() and int(declared) != size:
            # A transfer cut short reads as an early end of the body, not an error: read it again.
            raise http.client.IncompleteRead(b"", int(declared) - size)
        return digest.hexdigest(), size


def open_new(path, mode, **options):
    """Opens a new file at path: whatever was there is removed first, and the file is created
    exclusively, so a symbolic link put there can never lead the write elsewhere."""
    remove_quietly(path)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_BINARY", 0), 0o644)
    return os.fdopen(descriptor, mode, **options)


def identity(path):
    """A file's identity on disk, the same under every name that leads to it."""
    status = os.lstat(path)
    return (status.st_dev, status.st_ino)


def remove_quietly(path):
    try:
        os.remove(path)
    except FileNotFoundError:
        pass


def run(arguments):
    selection_id = str(uuid.UUID(arguments.selection))
    api = arguments.api.rstrip("/")
    bases = storage_bases(api, arguments.storage_base)
    selection = read_selection(api, selection_id)
    catalog_date = rfc3339_seconds(selection["catalog_date"])
    layout = Layout(selection_id)
    root = os.path.join(arguments.out, layout.root)
    if os.path.islink(root):
        raise ScriptError("{} is a symbolic link; choose where the files go with --out instead".format(root))
    os.makedirs(root, exist_ok=True)
    expected_root = os.path.join(os.path.realpath(arguments.out), layout.root)
    if same_name_key(os.path.realpath(root)) != same_name_key(expected_root):
        raise ScriptError("{} leads to {} (a link or junction); choose where the files go with --out "
                          "instead".format(root, os.path.realpath(root)))
    saver = Saver(arguments.out, root)
    layout.taken = saver.taken
    saver.start(selection_id, arguments.files)
    started_at = now_rfc3339()

    with tempfile.TemporaryFile("w+", encoding="utf-8") as listing:
        say("Listing selection {} ({:,} models)...".format(selection_id, selection["model_count"]))
        list_models(api, selection_id, listing)
        # The archive keeps every model's folder from the files' names before naming any file.
        listing.seek(0)
        for line in listing:
            layout.reserve_folder(layout.model_folder(json.loads(line)))
        listing.seek(0)

        walk = Walk(arguments.files, layout, bases, saver.read)
        counts = {INCLUDED: 0, LINKED: 0, UNAVAILABLE: 0}
        # Distinct files by reuse terms, and those not verified at save time, as the preview counts.
        terms = {CC0_NAME: set(), NOT_CONFIRMED_TERMS: set(), EXTERNAL_TERMS: set()}
        not_verified_at_save = set()
        not_saved = []
        manifest_path = os.path.join(root, "manifest.tsv")
        partial = manifest_path + ".part"
        with open_new(partial, "w", encoding="utf-8", newline="\n") as manifest:
            for line in manifest_header(selection_id, catalog_date, arguments.files, started_at):
                manifest.write(line + "\n")
            for line in listing:
                for row in walk.rows(json.loads(line)):
                    manifest.write(manifest_line(row))
                    if row["record_type"] != "file":
                        continue
                    counts[row["status"]] += 1
                    terms[row["license"]].add(row["file_id"])
                    if row["verified_at_save"] == "no":
                        not_verified_at_save.add(row["file_id"])
                    if row["reason_code"] in READ_REASONS:
                        not_saved.append(row)
        os.replace(partial, manifest_path)
    displaced = saver.finish()

    say("")
    say("Selection {}, file choice {}: {:,} included ({:,} downloaded, {:,} already saved), {:,} linked, "
        "{:,} unavailable.".format(
            selection_id, arguments.files, counts[INCLUDED], saver.saved, saver.kept, counts[LINKED],
            counts[UNAVAILABLE]))
    say("Reuse terms: {:,} files {}; {:,} files: {}; {:,} files held by other repositories: {}.".format(
        len(terms[CC0_NAME]), CC0_NAME, len(terms[NOT_CONFIRMED_TERMS]), NOT_CONFIRMED_TERMS,
        len(terms[EXTERNAL_TERMS]), EXTERNAL_TERMS))
    if not_verified_at_save:
        say("{:,} files {} (manifest.tsv: verified_at_save no).".format(
            len(not_verified_at_save), NOT_VERIFIED_AT_SAVE))
    say("Files and manifest.tsv: {}".format(root))
    if displaced:
        say("Files an earlier run saved that this run does not use: {:,}, moved to {}. Delete that "
            "folder when you no longer need them.".format(displaced, saver.displaced))
    if not_saved:
        say("{:,} files Dynamic PDB holds were not saved; manifest.tsv lists why. Run the script again "
            "to try them again:".format(len(not_saved)))
        for row in not_saved[:20]:
            say("  {} ({}): {}".format(row["file_name"], row["file_id"], row["reason"]))
        if len(not_saved) > 20:
            say("  ... and {:,} more".format(len(not_saved) - 20))
    say("")
    say("{} Files deposited by others: {}. {}".format(CC0_STATEMENT, NOT_CONFIRMED_TERMS, ACKNOWLEDGEMENT_REQUEST))
    return 1 if not_saved else 0


def parse_arguments(argv):
    parser = argparse.ArgumentParser(
        description="Download a saved Dynamic PDB selection file by file, with the archive's rules and no size limit.",
        epilog="{} Files deposited by others: {}. {}".format(CC0_STATEMENT, NOT_CONFIRMED_TERMS, ACKNOWLEDGEMENT_REQUEST),
    )
    parser.add_argument("--selection", required=True, help="the selection ID the download panel shows")
    parser.add_argument("--files", required=True, choices=FILE_CHOICES, help="which files to download")
    parser.add_argument("--out", default=".",
                        help="folder to write into (default: the current folder); files go under "
                             "<out>/dynamic-pdb-<selection id>/, one file choice per folder")
    parser.add_argument("--api", default=DEFAULT_API, help="the Dynamic PDB API (default: %(default)s)")
    parser.add_argument("--storage-base", action="append",
                        help="where the API's deployment keeps files; needed only for an API other than "
                             "dynamicpdb.com and dev.dynamicpdb.com (repeat for several)")
    arguments = parser.parse_args(argv)
    try:
        uuid.UUID(arguments.selection)
    except ValueError:
        parser.error("--selection must be a selection ID, for example 3f0c2a5e-8d1b-4c7a-9e2f-1a2b3c4d5e6f")
    return arguments


def main(argv=None):
    if os.name == "nt":
        # Windows names files in ways this script does not model (trailing dots, device names,
        # short names, junctions), so it does not write there at all.
        say("dynamic-pdb-download: " + WINDOWS_MESSAGE)
        return 2
    arguments = parse_arguments(sys.argv[1:] if argv is None else argv)
    try:
        return run(arguments)
    except ScriptError as error:
        say("dynamic-pdb-download: " + str(error))
        return 2
    except OSError as error:
        # The output folder cannot be created, read or written (a file read from the server that
        # fails is handled where it is read).
        say("dynamic-pdb-download: {}".format(error))
        return 2
    except KeyboardInterrupt:
        say("Stopped. Run the script again to finish: files already saved are kept.")
        return 2


if __name__ == "__main__":
    sys.exit(main())
