// The download script (public/download/dynamic-pdb-download.py): its rules case by case, then
// whole runs against a fake API and file store on 127.0.0.1. No network. The browser test checks
// the same script against a real archive of the same selection.
require("./register.cjs");

const assert = require("node:assert/strict");
const { spawn, spawnSync } = require("node:child_process");
const crypto = require("node:crypto");
const fs = require("node:fs");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const SCRIPT = path.join(__dirname, "..", "public", "download", "dynamic-pdb-download.py");

/**
 * Loads the script as a module and runs `body` with it as `script`. Waits are skipped so retries
 * are instant, and listed on stderr as "SLEPT [seconds, ...]". -I keeps the user's Python settings and the current folder out; -B writes no
 * bytecode next to the script, which the website serves as it is.
 */
const HARNESS = `
import importlib.util, json, sys
spec = importlib.util.spec_from_file_location("dynamic_pdb_download", sys.argv[1])
script = importlib.util.module_from_spec(spec)
spec.loader.exec_module(script)
import atexit
slept = []
script.time.sleep = slept.append
atexit.register(lambda: sys.stderr.write("SLEPT " + json.dumps(slept) + "\\n"))
exec(sys.stdin.read())
`;

function python(body, args = []) {
  const result = spawnSync("python3", ["-I", "-B", "-c", HARNESS, SCRIPT, ...args], { input: body, encoding: "utf8" });
  if (result.status !== 0) {
    throw new Error(`python3 failed (${result.status}): ${result.stderr}`);
  }
  return JSON.parse(result.stdout);
}

/** Runs the script's main() with these arguments; resolves with its exit status and output. */
function runScript(args) {
  return new Promise((resolve, reject) => {
    const child = spawn("python3", ["-I", "-B", "-c", HARNESS, SCRIPT], { stdio: ["pipe", "pipe", "pipe"] });
    let stderr = "";
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.on("error", reject);
    child.on("close", (status) => resolve({ status, stderr }));
    child.stdin.end(`sys.exit(script.main(${JSON.stringify(args)}))`);
  });
}

test("the script compiles with the standard library only", () => {
  const result = spawnSync(
    "python3",
    ["-I", "-B", "-c", "import sys; compile(open(sys.argv[1], encoding='utf-8').read(), sys.argv[1], 'exec')", SCRIPT],
    { encoding: "utf8" },
  );
  assert.equal(result.status, 0, result.stderr);
  const imports = fs
    .readFileSync(SCRIPT, "utf8")
    .split("\n")
    .filter((line) => /^(import|from) /.test(line))
    .map((line) => line.split(/\s+/)[1].split(".")[0]);
  const standard = ["argparse", "hashlib", "http", "json", "ntpath", "os", "re", "sys", "tempfile", "time", "unicodedata", "urllib", "uuid", "datetime"];
  assert.deepEqual(
    imports.filter((name) => !standard.includes(name)),
    [],
  );
});

test("the script says the CC0 text exactly as the archive and the page say it", () => {
  const { REUSE_TERMS } = require("../src/lib/download-selection.ts");
  const result = python("print(json.dumps(script.CC0_STATEMENT))");
  assert.equal(result, REUSE_TERMS);
  const worker = fs.readFileSync(
    path.join(__dirname, "..", "..", "backend", "cmd", "archive-worker", "worker", "manifest.go"),
    "utf8",
  );
  const goText = /cc0Statement = "([^"]+)" \+\s*"([^"]+)"/.exec(worker);
  assert.ok(goText, "the worker's CC0 statement");
  assert.equal(goText[1] + goText[2], REUSE_TERMS);
});

test("the script's manifest has the archive's columns, in order", () => {
  const worker = fs.readFileSync(
    path.join(__dirname, "..", "..", "backend", "cmd", "archive-worker", "worker", "manifest.go"),
    "utf8",
  );
  const block = /var manifestColumns = \[\]string\{([\s\S]*?)\n\}/.exec(worker);
  assert.ok(block, "the worker's manifest columns");
  const goColumns = [...block[1].matchAll(/"([a-z_0-9]+)"/g)].map((match) => match[1]);
  assert.deepEqual(python("print(json.dumps(list(script.MANIFEST_COLUMNS)))"), goColumns);
});

// ---------------------------------------------------------------------------------------------
// Rules, case by case.
// ---------------------------------------------------------------------------------------------

const RULES = `
def model(model_type, origin=None):
    return {"model_type": model_type, "origin": origin}

def file(uri, type="model", roles=("primary",)):
    return {"uri": uri, "type": type, "roles": list(roles)}

rcsb = file("https://files.rcsb.org/download/1ABC.cif")
rcsb_sf = file("https://files.rcsb.org/download/1ABC-sf.cif")
ours = file("https://files.example/e1/model.cif")
entry_fasta = file("https://files.example/e1/seq.fasta", type="fasta", roles=("entry",))
entry_cif = file("https://files.rcsb.org/download/1ABC.cif", roles=("entry",))
run_output = file("https://files.example/e1/out.cif", roles=("run_output",))
deposited, computed, rerefined, untyped = model("Deposited", "archive"), model("Ensemble"), model("Ensemble", "archive"), model(None)
out = {}
for name, args in {
    "original: the PDB's file of a Deposited model": ("original_mmcif", rcsb, deposited),
    "original: the PDB's file of a re-refined archive model": ("original_mmcif", rcsb, rerefined),
    "original: not structure factors": ("original_mmcif", rcsb_sf, deposited),
    "original: not our coordinates": ("original_mmcif", ours, computed),
    "original: not through the entry": ("original_mmcif", entry_cif, deposited),
    "modified: our coordinates of a computed model": ("modified_coordinates", ours, computed),
    "modified: a run's output coordinates": ("modified_coordinates", run_output, computed),
    "modified: not the PDB's file": ("modified_coordinates", rcsb, computed),
    "modified: not a re-refined archive model": ("modified_coordinates", ours, rerefined),
    "modified: not a Deposited model": ("modified_coordinates", ours, deposited),
    "modified: not a model without a type": ("modified_coordinates", ours, untyped),
    "modified: not the entry's files": ("modified_coordinates", entry_fasta, computed),
    "all: the entry's files too": ("all_files", entry_fasta, deposited),
}.items():
    out[name] = script.selects_file(*args)
print(json.dumps(out))
`;

test("each file choice selects the files the archive selects", () => {
  assert.deepEqual(python(RULES), {
    "original: the PDB's file of a Deposited model": true,
    "original: the PDB's file of a re-refined archive model": true,
    "original: not structure factors": false,
    "original: not our coordinates": false,
    "original: not through the entry": false,
    "modified: our coordinates of a computed model": true,
    "modified: a run's output coordinates": true,
    "modified: not the PDB's file": false,
    "modified: not a re-refined archive model": false,
    "modified: not a Deposited model": false,
    "modified: not a model without a type": false,
    "modified: not the entry's files": false,
    "all: the entry's files too": true,
  });
});

const STATES = `
bases = [script.parse_storage_base("https://files.example/bucket")]
ours, copy, elsewhere = "https://files.example/bucket/e1/a.cif", "https://files.example/bucket/copies/b.cif", "https://other.example/c.cif"
def checked(status, copy_status, uri=ours, stored_uri=None):
    return {"id": "f", "uri": uri, "recorded_sha256": "ab" * 32, "checksum": {
        "status": status, "copy_status": copy_status, "stored_uri": stored_uri, "size_bytes": 5,
        "checked_at": "2026-11-16T10:00:00.5Z" if status != "pending" else None}}
out = {}
for name, file in {
    "our upload, verified": checked("verified", "own_storage"),
    "our copy of an external file": checked("verified", "copied", uri=elsewhere, stored_uri=copy),
    "a copy outside our storage": checked("verified", "copied", uri=elsewhere, stored_uri=elsewhere),
    "over 1 GiB": checked("verified", "over_limit", uri=elsewhere),
    "copy failed": checked("verified", "copy_failed", uri=elsewhere),
    "copying off": checked("verified", "copy_off", uri=elsewhere),
    "our file differs": checked("mismatch", "own_storage"),
    "changed at its source": checked("mismatch", "mismatch", uri=elsewhere),
    "not checked yet": checked("pending", None),
    "unreadable at its source": checked("failed", None, uri=elsewhere),
}.items():
    out[name] = list(script.state_of(file, bases))
errors = {}
for name, file in {
    "unknown checksum status": checked("lost", None),
    "unknown copy status": checked("verified", "teleported"),
    "verified without a copy status": checked("verified", None),
    "a verified result with copy status mismatch": checked("verified", "mismatch"),
}.items():
    try:
        script.state_of(file, bases)
        errors[name] = None
    except script.ScriptError as error:
        errors[name] = str(error)
for name, model in {
    "unknown model type": {"model_type": "Mystery", "origin": None},
    "unknown model origin": {"model_type": "Ensemble", "origin": "future-origin"},
}.items():
    try:
        script.is_computed_model(model)
        errors[name] = None
    except script.ScriptError as error:
        errors[name] = str(error)
print(json.dumps({"states": out, "errors": errors}))
`;

test("a file's status before reading follows the checksum job's last check, as in the archive", () => {
  const { states, errors } = python(STATES);
  const at = "2026-11-16T10:00:00.5Z";
  assert.deepEqual(states, {
    "our upload, verified": ["included", "verified", null, "", "https://files.example/bucket/e1/a.cif"],
    "our copy of an external file": ["included", "verified", null, "", "https://files.example/bucket/copies/b.cif"],
    "a copy outside our storage": ["unavailable", "not_verified", null, "copy_not_in_storage", null],
    "over 1 GiB": ["linked", "verified", at, "over_copy_limit", null],
    "copy failed": ["linked", "verified", at, "copy_failed", null],
    "copying off": ["linked", "verified", at, "copy_off", null],
    "our file differs": ["unavailable", "mismatch", at, "stored_file_differs", null],
    "changed at its source": ["changed-at-source", "mismatch", at, "changed_at_source", null],
    "not checked yet": ["linked", "not_verified", null, "not_checked_yet", null],
    "unreadable at its source": ["unavailable", "not_verified", at, "source_unreadable", null],
  });
  // Unknown or impossible values stop the run instead of guessing.
  assert.match(errors["unknown checksum status"], /unknown checksum status: 'lost'/);
  assert.match(errors["unknown copy status"], /unknown copy status: 'teleported'/);
  assert.match(errors["verified without a copy status"], /has no copy status/);
  assert.match(errors["a verified result with copy status mismatch"], /copy status mismatch/);
  assert.match(errors["unknown model type"], /unknown model type: 'Mystery'/);
  assert.match(errors["unknown model origin"], /unknown model origin: 'future-origin'/);
});

test("a location is ours only under a storage base, as the worker maps it to a key", () => {
  const result = python(`
bases = [script.parse_storage_base(b) for b in ("https://files.example", "http://127.0.0.1:9000/bucket/")]
print(json.dumps({uri: script.in_storage(uri, bases) for uri in [
    "https://files.example/e1/a.cif",
    "HTTPS://FILES.EXAMPLE/e1/a.cif",
    "https://files.example/",
    "https://files.example/e1/a.cif?x=1",
    "https://user@files.example/e1/a.cif",
    "http://127.0.0.1:9000/bucket/e1/a.cif",
    "http://127.0.0.1:9000/bucket2/e1/a.cif",
    "http://127.0.0.1:9000/other/../bucket/e1/a.cif",
    "https://files.rcsb.org/download/1ABC.cif",
]}))
`);
  assert.deepEqual(result, {
    "https://files.example/e1/a.cif": true,
    "HTTPS://FILES.EXAMPLE/e1/a.cif": true,
    "https://files.example/": false,
    "https://files.example/e1/a.cif?x=1": false,
    "https://user@files.example/e1/a.cif": false,
    "http://127.0.0.1:9000/bucket/e1/a.cif": true,
    "http://127.0.0.1:9000/bucket2/e1/a.cif": false,
    "http://127.0.0.1:9000/other/../bucket/e1/a.cif": true,
    "https://files.rcsb.org/download/1ABC.cif": false,
  });
});

test("each deployment's API picks where that deployment keeps files, as its backend config says", () => {
  const expected = {};
  for (const name of ["production", "dev"]) {
    const config = fs.readFileSync(path.join(__dirname, "..", "..", "backend", "config", `${name}.yml`), "utf8");
    const cdn = /\ncdn:\n((?:[ #].*\n)*)/.exec(config)[1];
    const api = /^ {2}public_base_url: (\S+)$/m.exec(config)[1];
    const region = /^ {4}region: (\S+)$/m.exec(cdn)[1];
    const bucket = /^ {4}bucket: (\S+)$/m.exec(cdn)[1];
    const cloudFront = /^ {2}cloudfront:\n {4}base_url: (\S+)$/m.exec(cdn)[1];
    expected[api] = [cloudFront, `https://${bucket}.s3.${region}.amazonaws.com`];
  }
  const result = python(`
files = ("https://dev-files.dynamicpdb.com/e1/a.cif", "https://files.dynamicpdb.com/e1/a.cif",
         "http://127.0.0.1:9000/bucket/e1/a.cif")
def ours(api, given=None):
    bases = script.storage_bases(api, given)
    return [script.in_storage(uri, bases) for uri in files]
out = {
    "table": {api: list(bases) for api, bases in script.DEPLOYMENT_STORAGE_BASES.items()},
    "production": ours("https://dynamicpdb.com/api"),
    "dev": ours("https://dev.dynamicpdb.com/api"),
    "given": ours("https://dev.dynamicpdb.com/api", ["http://127.0.0.1:9000/bucket"]),
}
try:
    script.storage_bases("http://127.0.0.1:8080/api", None)
    out["unknown"] = None
except script.ScriptError as error:
    out["unknown"] = str(error)
print(json.dumps(out))
`);
  assert.deepEqual(result.table, expected);
  assert.deepEqual(result.production, [false, true, false]);
  assert.deepEqual(result.dev, [true, false, false]);
  assert.deepEqual(result.given, [false, false, true], "--storage-base replaces the deployment's");
  assert.match(result.unknown, /^http:\/\/127\.0\.0\.1:8080\/api is not a Dynamic PDB deployment .*--storage-base$/);
});

test("files get the archive's paths: one per file, prefixed by the file ID when taken", () => {
  const result = python(`
layout = script.Layout("sel")
first, second = "11111111-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "22222222-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
third, fourth = "33333333-cccc-4ccc-8ccc-cccccccccccc", "44444444-dddd-4ddd-8ddd-dddddddddddd"
m1 = {"entry_id": "E1", "model_id": "E1_m_1"}
m2 = {"entry_id": "E1", "model_id": "E1_m_2"}
layout.reserve_folder(layout.model_folder(m1))
layout.reserve_folder(layout.model_folder(m2))
f = lambda id, name: {"id": id, "name": name}
out = {
    "first": layout.path(f(first, "model.cif"), m1, False),
    "the same file again": layout.path(f(first, "model.cif"), m2, False),
    "a second file of the name": layout.path(f(second, "model.cif"), m1, False),
    "a third": layout.path(f(third, "model.cif"), m1, False),
    "a name differing only in case": layout.path(f(fourth, "MODEL.cif"), m1, False),
}
peek = layout.path(f("55555555-eeee-4eee-8eee-eeeeeeeeeeee", "x.cif"), m1, False, claim=False)
out["a path looked at, not taken"] = [peek, layout.path(f("66666666-ffff-4fff-8fff-ffffffffffff", "x.cif"), m1, False)]
print(json.dumps(out))
`);
  assert.deepEqual(result, {
    first: "dynamic-pdb-sel/E1/E1_m_1/model.cif",
    "the same file again": "dynamic-pdb-sel/E1/E1_m_1/model.cif",
    "a second file of the name": "dynamic-pdb-sel/E1/E1_m_1/22222222_model.cif",
    "a third": "dynamic-pdb-sel/E1/E1_m_1/33333333_model.cif",
    "a name differing only in case": "dynamic-pdb-sel/E1/E1_m_1/44444444_MODEL.cif",
    "a path looked at, not taken": ["dynamic-pdb-sel/E1/E1_m_1/x.cif", "dynamic-pdb-sel/E1/E1_m_1/x.cif"],
  });
});

test("an entry file named like a model takes the ID prefix, so no file takes a model folder's path", () => {
  const result = python(`
layout = script.Layout("sel")
model = {"entry_id": "E1", "model_id": "E1_m_2"}
layout.reserve_folder(layout.model_folder(model))
print(json.dumps(layout.path({"id": "77777777-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "name": "e1_M_2"}, model, True)))
`);
  assert.equal(result, "dynamic-pdb-sel/E1/77777777_e1_M_2");
});

test("file names stay inside their folder and within 255 bytes, as in the archive", () => {
  const result = python(`
fid = "88888888-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
names = {
    "a path": "../../etc/passwd",
    "backslashes": "dir\\\\evil.cif",
    "control characters": "\\u0001bad\\u007f.cif",
    "blank": "   ",
    "dot dot": "..",
}
out = {key: script.safe_file_name(value, fid) for key, value in names.items()}
long_ascii = script.bounded_file_name("", "a" * 300 + ".cif")
long_utf8 = script.bounded_file_name("", "\\u00e9" * 200 + ".cif")
long_extension = script.bounded_file_name("", "b" * 250 + "." + "x" * 40)
out["long ascii"] = [len(long_ascii.encode()), long_ascii[-6:]]
out["long utf-8"] = [len(long_utf8.encode()), long_utf8[-6:]]
out["long extension"] = [len(long_extension.encode()), long_extension[-3:]]
print(json.dumps(out))
`);
  assert.deepEqual(result, {
    "a path": "passwd",
    backslashes: "evil.cif",
    "control characters": "bad.cif",
    blank: "88888888-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
    "dot dot": "88888888-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
    "long ascii": [255, "aa.cif"],
    "long utf-8": [254, "éé.cif"],
    "long extension": [255, "xxx"],
  });
});

test("times are written as the archive writes them: UTC, whole seconds", () => {
  const result = python(`
print(json.dumps([script.rfc3339_seconds(value) for value in [
    "2026-11-16T10:00:00.123456789Z", "2026-11-16T12:30:05+02:30", "2026-11-16T00:00:00-01:00", None]]))
`);
  assert.deepEqual(result, ["2026-11-16T10:00:00Z", "2026-11-16T10:00:05Z", "2026-11-16T01:00:00Z", ""]);
});

test("an entry or model ID that cannot be a folder name stops the run", () => {
  const result = python(`
out = []
for value in ["..", "a/b", "a\\\\b", ""]:
    try:
        script.check_path_part("entry ID", value)
        out.append(None)
    except script.ScriptError as error:
        out.append(str(error))
print(json.dumps(out))
`);
  assert.equal(result.length, 4);
  for (const message of result) {
    assert.match(message, /cannot be a folder name/);
  }
});

// ---------------------------------------------------------------------------------------------
// Whole runs against a fake API and file store.
// ---------------------------------------------------------------------------------------------

const SELECTION = "3f0c2a5e-8d1b-4c7a-9e2f-1a2b3c4d5e6f";
const SELECTION_OFF = "00000000-0000-4000-8000-000000000001";
const SELECTION_MYSTERY = "00000000-0000-4000-8000-000000000002";
const SELECTION_CUT_404 = "00000000-0000-4000-8000-000000000003";

function sha256(content) {
  return crypto.createHash("sha256").update(content).digest("hex");
}

/** A fake Dynamic PDB: two pages of a selection's models, and the files they name. */
function fakeServer() {
  const files = new Map(); // path -> { status, content }
  const requests = new Map(); // path -> count
  const truncate = new Set(); // paths whose next answer ends early
  const state = { base: "" };
  const byID = {};

  function selectionFile(id, name, options) {
    const content = Buffer.from(options.content ?? `${name} of ${id}\n`);
    const ours = options.ours ?? true;
    const storedPath = `/bucket/${id}/${name}`;
    const uri = ours ? `${state.base}${storedPath}` : options.uri;
    const checksum = options.checksum ?? {
      status: "verified",
      sha256: sha256(content),
      size_bytes: content.length,
      stored_uri: options.copied ? `${state.base}/bucket/copies/${name}` : null,
      copy_status: options.copied ? "copied" : "own_storage",
      checked_at: "2026-11-15T09:30:00.123456Z",
    };
    if (checksum.status === "verified" && (checksum.copy_status === "own_storage" || checksum.copy_status === "copied")) {
      const served = options.copied ? `/bucket/copies/${name}` : storedPath;
      files.set(served, { status: options.serveStatus ?? 200, content: options.servedContent ?? content });
    }
    const file = {
      id,
      roles: options.roles ?? ["primary"],
      name,
      level: options.level ?? "L2",
      type: options.type ?? "model",
      format: options.format ?? "mmcif",
      uri,
      size_bytes: content.length,
      recorded_sha256: checksum.sha256 ?? null,
      recorded_sha256_source: checksum.sha256 ? "declared" : "not_recorded",
      recorded_after_save: false,
      checksum,
    };
    byID[name] = { file, content };
    return file;
  }

  function build() {
    const id = (n) => `${String(n).padStart(8, "0")}-0000-4000-8000-${String(n).padStart(12, "0")}`;
    const fasta = selectionFile(id(1), "E1.fasta", { roles: ["entry"], type: "fasta", format: "fasta", level: "L0" });
    const rcsb = selectionFile(id(2), "1ABC.cif", { ours: false, copied: true, uri: "https://files.rcsb.org/download/1ABC.cif" });
    const pendingSF = selectionFile(id(3), "1ABC-sf.cif", {
      ours: false,
      uri: "https://files.rcsb.org/download/1ABC-sf.cif",
      roles: ["model"],
      type: "structure_factors",
      checksum: { status: "pending", sha256: null, size_bytes: null, stored_uri: null, copy_status: null, checked_at: null },
    });
    const m2Model = selectionFile(id(4), "model.cif", {});
    const big = selectionFile(id(5), "big.tar", {
      ours: false,
      uri: "https://zenodo.example/big.tar",
      roles: ["run_output"],
      type: "other",
      format: null,
      checksum: {
        status: "verified",
        sha256: "c".repeat(64),
        size_bytes: 2_000_000_000,
        stored_uri: null,
        copy_status: "over_limit",
        checked_at: "2026-11-15T09:31:00Z",
      },
    });
    const differs = selectionFile(id(6), "differs.dat", {
      roles: ["model"],
      type: "other",
      format: null,
      servedContent: Buffer.from("not the recorded bytes\n"),
    });
    const missing = selectionFile(id(7), "missing.dat", { roles: ["model"], type: "other", format: null, serveStatus: 404 });
    const flaky = selectionFile(id(8), "flaky.dat", { roles: ["model"], type: "other", format: null, serveStatus: 500 });
    const changed = selectionFile(id(9), "changed.cif", {
      ours: false,
      uri: "https://elsewhere.example/changed.cif",
      roles: ["run_input"],
      type: "other",
      checksum: {
        status: "mismatch",
        sha256: "d".repeat(64),
        size_bytes: 10,
        stored_uri: null,
        copy_status: "mismatch",
        checked_at: "2026-11-15T09:32:00+01:00",
      },
    });
    const m3First = selectionFile(id(10), "model.cif", { content: "first ensemble file\n" });
    const m3Second = selectionFile(id(11), "model.cif", { roles: ["model"], content: "second ensemble file\n" });
    const e2Named = selectionFile(id(12), "E2_m_003", { roles: ["entry"], type: "other", format: null, level: "L0" });
    const rcsb2 = selectionFile(id(13), "2XYZ.cif", { ours: false, copied: true, uri: "https://files.rcsb.org/download/2XYZ.cif" });
    // A run's input named like the model's coordinates: all_files gives it the plain name,
    // modified_coordinates (which leaves it out) gives the plain name to the coordinates.
    const m5Input = selectionFile(id(14), "coords.cif", { roles: ["run_input"], type: "other", content: "input\n" });
    const m5Coordinates = selectionFile(id(15), "coords.cif", { content: "coordinates\n" });
    byID["m5Input"] = { file: m5Input };
    byID["m5Coordinates"] = { file: m5Coordinates };
    // byID keys are names; the second "model.cif" replaced the first, so keep both by ID too.
    byID["m3First"] = { file: m3First };
    byID["m3Second"] = { file: m3Second };
    byID["m2Model"] = { file: m2Model };
    const model = (entry, modelID, number, type, origin, modelFiles) => ({
      model_id: modelID,
      entry_id: entry,
      model_revision_number: number,
      model_type: type,
      origin,
      entry_revision_number: 3,
      files: modelFiles,
    });
    return [
      [
        model("E1", "E1_m_001", 1, "Deposited", "archive", [rcsb, pendingSF, fasta]),
        model("E1", "E1_m_002", 2, "Single Conformer", null, [m2Model, big, differs, missing, flaky, changed, fasta]),
      ],
      [
        model("E2", "E2_m_003", 1, "Ensemble", null, [m3First, m3Second, e2Named]),
        model("E2", "E2_m_004", 1, "Ensemble", "archive", [rcsb2]),
        model("E3", "E3_m_005", 1, "Ensemble", null, [m5Input, m5Coordinates]),
      ],
    ];
  }

  let pages = [];
  const server = http.createServer((request, response) => {
    const url = new URL(request.url, "http://127.0.0.1");
    requests.set(url.pathname, (requests.get(url.pathname) ?? 0) + 1);
    if (truncate.delete(url.pathname)) {
      // Fewer bytes than Content-Length, then the connection closes: Python's IncompleteRead.
      response.writeHead(200, { "content-type": "application/vnd.api+json", "content-length": "5000" });
      response.write('{"data": {', () => response.socket.destroy());
      return;
    }
    const json = (status, body) => {
      response.writeHead(status, { "content-type": "application/vnd.api+json" });
      response.end(JSON.stringify(body));
    };
    const problem = (status, code) => json(status, { errors: [{ status: String(status), code, detail: code }] });
    const selection = /^\/api\/v1\/selections\/([^/]+)(\/models)?$/.exec(url.pathname);
    if (selection) {
      const [, selectionID, models] = selection;
      if (selectionID === SELECTION_OFF) return problem(404, "SELECTIONS_OFF");
      if (selectionID === SELECTION_CUT_404) {
        // A 404 whose body ends early.
        response.writeHead(404, { "content-type": "application/vnd.api+json", "content-length": "5000" });
        response.write('{"errors": [', () => response.socket.destroy());
        return;
      }
      if (selectionID === SELECTION_MYSTERY) {
        if (!models) return json(200, selectionDocument(selectionID, 1));
        return json(200, {
          data: [{ type: "selection-models", id: "M", attributes: { ...pages[1][0], model_type: "Mystery" } }],
        });
      }
      if (selectionID !== SELECTION) return problem(404, "NOT_FOUND");
      if (!models) return json(200, selectionDocument(selectionID, 5));
      assert.equal(url.searchParams.get("limit"), "100");
      const second = url.searchParams.get("cursor") === "page-2";
      const page = pages[second ? 1 : 0];
      return json(200, {
        data: page.map((attributes) => ({ type: "selection-models", id: attributes.model_id, attributes })),
        meta: second ? {} : { next_cursor: "page-2" },
      });
    }
    const stored = files.get(url.pathname);
    if (!stored) {
      response.writeHead(404);
      return response.end();
    }
    if (stored.once) {
      const once = stored.once;
      delete stored.once;
      if (once.truncate) {
        // Half the body, then the connection closes: Python sees an early end, not an error.
        response.writeHead(200, { "content-length": stored.content.length });
        response.write(stored.content.subarray(0, Math.floor(stored.content.length / 2)), () => response.socket.destroy());
        return;
      }
      response.writeHead(once.status, once.headers ?? {});
      return response.end("busy");
    }
    if (stored.status !== 200) {
      response.writeHead(stored.status);
      return response.end("no");
    }
    response.writeHead(200, { "content-length": stored.content.length });
    response.end(stored.content);
  });

  return {
    async start() {
      await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
      state.base = `http://127.0.0.1:${server.address().port}`;
      pages = build();
      return state.base;
    },
    stop: () => new Promise((resolve) => server.close(resolve)),
    files,
    requests,
    truncate,
    byID,
  };
}

function selectionDocument(id, modelCount) {
  return {
    data: {
      type: "selections",
      id,
      attributes: {
        model_count: modelCount,
        entry_count: 2,
        created_at: "2026-11-16T08:00:00.987Z",
        catalog_date: "2026-11-16T08:00:00.987Z",
        request: { model_ids: [], searches: [], exclude_model_ids: [], add_deposited_models: false },
      },
      links: { self: "", models: "" },
    },
  };
}

/** manifest.tsv's comment lines and its rows as objects. */
function readManifest(folder) {
  const lines = fs.readFileSync(path.join(folder, `dynamic-pdb-${SELECTION}`, "manifest.tsv"), "utf8").trimEnd().split("\n");
  const comments = lines.filter((line) => line.startsWith("#"));
  const [header, ...rows] = lines.filter((line) => !line.startsWith("#"));
  const columns = header.split("\t");
  return {
    comments,
    rows: rows.map((line) => Object.fromEntries(line.split("\t").map((value, index) => [columns[index], value]))),
  };
}

function scriptArgs(base, out, files, selection = SELECTION) {
  return ["--selection", selection, "--files", files, "--out", out, "--api", `${base}/api`, "--storage-base", `${base}/bucket`];
}

test("the script downloads a selection file by file with the archive's statuses, and finishes the rest when run again", async (t) => {
  const server = fakeServer();
  const base = await server.start();
  t.after(() => server.stop());
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-script-"));
  t.after(() => fs.rmSync(out, { recursive: true, force: true }));
  const root = path.join(out, `dynamic-pdb-${SELECTION}`);

  // when
  const first = await runScript(scriptArgs(base, out, "all_files"));

  // then the files Dynamic PDB holds are saved and checked, the rest listed with why
  assert.equal(first.status, 1, first.stderr);
  const { comments, rows } = readManifest(out);
  assert.ok(comments.includes(`# These data are dedicated to the public domain under CC0 1.0. We ask that you acknowledge Dynamic PDB and cite the depositors of the entries you use.`));
  const summary = rows.map((row) => [row.record_type, row.model_id, row.file_name, row.status, row.reason_code, row.path_in_archive]);
  const prefix = `dynamic-pdb-${SELECTION}`;
  assert.deepEqual(summary, [
    ["file", "E1_m_001", "1ABC.cif", "included", "", `${prefix}/E1/E1_m_001/1ABC.cif`],
    ["file", "E1_m_001", "1ABC-sf.cif", "linked", "not_checked_yet", ""],
    ["file", "", "E1.fasta", "included", "", `${prefix}/E1/E1.fasta`],
    ["file", "E1_m_002", "model.cif", "included", "", `${prefix}/E1/E1_m_002/model.cif`],
    ["file", "E1_m_002", "big.tar", "linked", "over_copy_limit", ""],
    ["file", "E1_m_002", "differs.dat", "unavailable", "copy_differs", ""],
    ["file", "E1_m_002", "missing.dat", "unavailable", "copy_missing", ""],
    ["file", "E1_m_002", "flaky.dat", "unavailable", "read_failed", ""],
    ["file", "E1_m_002", "changed.cif", "changed-at-source", "changed_at_source", ""],
    ["file", "E2_m_003", "model.cif", "included", "", `${prefix}/E2/E2_m_003/model.cif`],
    ["file", "E2_m_003", "model.cif", "included", "", `${prefix}/E2/E2_m_003/00000011_model.cif`],
    ["file", "", "E2_m_003", "included", "", `${prefix}/E2/00000012_E2_m_003`],
    ["file", "E2_m_004", "2XYZ.cif", "included", "", `${prefix}/E2/E2_m_004/2XYZ.cif`],
    ["file", "E3_m_005", "coords.cif", "included", "", `${prefix}/E3/E3_m_005/coords.cif`],
    ["file", "E3_m_005", "coords.cif", "included", "", `${prefix}/E3/E3_m_005/00000015_coords.cif`],
  ]);
  const byName = Object.fromEntries(rows.map((row) => [`${row.model_id}/${row.file_name}`, row]));
  assert.equal(byName["E1_m_002/differs.dat"].verification, "mismatch");
  assert.equal(byName["E1_m_002/big.tar"].source, "external");
  assert.equal(byName["E1_m_002/big.tar"].license, "See the repository's terms");
  assert.equal(byName["E1_m_001/1ABC.cif"].source, "pdb_archive");
  assert.equal(byName["E1_m_001/1ABC.cif"].license, "CC0 1.0");
  assert.equal(byName["E1_m_002/model.cif"].source, "dynamic_pdb_upload");
  assert.equal(byName["E1_m_002/model.cif"].model_type, "Single Conformer");
  assert.equal(byName["E1_m_002/model.cif"].model_revision, "2");
  assert.equal(byName["E1_m_002/model.cif"].entry_revision, "3");
  assert.equal(byName["E1_m_002/changed.cif"].verified_at, "2026-11-15T08:32:00Z");
  assert.equal(byName["/E1.fasta"].model_revision, "", "an entry file's row has no model columns");
  for (const row of rows.filter((candidate) => candidate.status === "included")) {
    const saved = fs.readFileSync(path.join(out, ...row.path_in_archive.split("/")));
    assert.equal(sha256(saved), row.recorded_sha256, row.path_in_archive);
  }
  assert.equal(server.requests.get(`/bucket/${server.byID["flaky.dat"].file.id}/flaky.dat`), 3, "read three times");
  assert.deepEqual(
    fs.readdirSync(root).filter((name) => name.startsWith(".incoming")),
    [],
    "no partial download is left behind",
  );
  assert.match(first.stderr, /3 files Dynamic PDB holds were not saved/);
  assert.match(first.stderr, /These data are dedicated to the public domain under CC0 1\.0/);

  // when the flaky file can be read again and the script runs again
  server.files.get(`/bucket/${server.byID["flaky.dat"].file.id}/flaky.dat`).status = 200;
  const modelPath = `/bucket/${server.byID["m2Model"].file.id}/model.cif`;
  const modelReads = server.requests.get(modelPath);
  const second = await runScript(scriptArgs(base, out, "all_files"));

  // then it saves the flaky file and keeps what it saved before without reading it again
  assert.equal(second.status, 1, "the missing and differing files are still not saved");
  const again = Object.fromEntries(readManifest(out).rows.map((row) => [`${row.model_id}/${row.file_name}`, row]));
  assert.equal(again["E1_m_002/flaky.dat"].status, "included");
  assert.equal(again["E1_m_002/flaky.dat"].path_in_archive, `${prefix}/E1/E1_m_002/flaky.dat`);
  assert.equal(server.requests.get(modelPath), modelReads, "a saved file is not downloaded again");
  assert.match(second.stderr, /\(1 downloaded, 9 already saved\)/);
});

test("a file an earlier run saved is never overwritten when a name clash resolves the other way", async (t) => {
  const server = fakeServer();
  const base = await server.start();
  t.after(() => server.stop());
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-script-"));
  t.after(() => fs.rmSync(out, { recursive: true, force: true }));
  const prefix = `dynamic-pdb-${SELECTION}`;
  const first = server.byID["m3First"].file;
  const second = server.byID["m3Second"].file;
  const firstPath = `/bucket/${first.id}/model.cif`;
  const secondPath = `/bucket/${second.id}/model.cif`;
  const rowsOf = () =>
    Object.fromEntries(readManifest(out).rows.filter((row) => row.model_id === "E2_m_003").map((row) => [row.file_id, row]));

  // given a first run where the first of two files named model.cif cannot be read, so the second takes the name
  server.files.get(firstPath).status = 500;
  await runScript(scriptArgs(base, out, "all_files"));
  assert.equal(rowsOf()[first.id].reason_code, "read_failed");
  assert.equal(rowsOf()[second.id].path_in_archive, `${prefix}/E2/E2_m_003/model.cif`);

  // when the first can be read again, and the second no longer can
  server.files.get(firstPath).status = 200;
  server.files.get(secondPath).status = 404;
  const again = await runScript(scriptArgs(base, out, "all_files"));

  // then the first takes the name, as in the archive, and the second keeps its saved bytes under its prefixed name
  const rows = rowsOf();
  assert.equal(rows[first.id].path_in_archive, `${prefix}/E2/E2_m_003/model.cif`);
  assert.equal(rows[second.id].status, "included", again.stderr);
  assert.equal(rows[second.id].path_in_archive, `${prefix}/E2/E2_m_003/00000011_model.cif`);
  for (const row of [rows[first.id], rows[second.id]]) {
    assert.equal(sha256(fs.readFileSync(path.join(out, ...row.path_in_archive.split("/")))), row.recorded_sha256);
  }
  assert.equal(server.requests.get(secondPath), 1, "taken back from the earlier run, not downloaded again");
  assert.equal(fs.existsSync(path.join(out, prefix, ".displaced")), false, "nothing is left aside");
});

test("a folder holds one file choice: the script will not mix another into it", async (t) => {
  const server = fakeServer();
  const base = await server.start();
  t.after(() => server.stop());
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-script-"));
  t.after(() => fs.rmSync(out, { recursive: true, force: true }));
  const root = path.join(out, `dynamic-pdb-${SELECTION}`);

  await runScript(scriptArgs(base, out, "all_files"));
  const before = fs.readFileSync(path.join(root, "manifest.tsv"), "utf8");
  const other = await runScript(scriptArgs(base, out, "modified_coordinates"));

  assert.equal(other.status, 2);
  assert.match(other.stderr, /holds a download of selection .* with file choice all_files; give file choice modified_coordinates its own --out/);
  assert.equal(fs.readFileSync(path.join(root, "manifest.tsv"), "utf8"), before, "nothing changed");

  // and an archive of another choice unpacked over it is noticed, though the mark still says all_files
  fs.writeFileSync(
    path.join(root, "manifest.tsv"),
    `# Dynamic PDB archive manifest. Selection ${SELECTION}, catalog date 2026-11-16T08:00:00Z, file choice original_mmcif, prepared 2026-11-16T09:00:00Z.\n`,
  );
  const overUnpacked = await runScript(scriptArgs(base, out, "all_files"));
  assert.equal(overUnpacked.status, 2);
  assert.match(overUnpacked.stderr, /with file choice original_mmcif; give file choice all_files its own --out/);

  // and an archive unpacked into the folder counts as its manifest says
  const unpacked = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-script-"));
  t.after(() => fs.rmSync(unpacked, { recursive: true, force: true }));
  fs.mkdirSync(path.join(unpacked, `dynamic-pdb-${SELECTION}`));
  fs.writeFileSync(
    path.join(unpacked, `dynamic-pdb-${SELECTION}`, "manifest.tsv"),
    `# Dynamic PDB archive manifest. Selection ${SELECTION}, catalog date 2026-11-16T08:00:00Z, file choice original_mmcif, prepared 2026-11-16T09:00:00Z.\n`,
  );
  const mixed = await runScript(scriptArgs(base, unpacked, "all_files"));
  assert.equal(mixed.status, 2);
  assert.match(mixed.stderr, /with file choice original_mmcif/);
  const same = await runScript(scriptArgs(base, unpacked, "original_mmcif"));
  assert.equal(same.status, 0, same.stderr);
});

test("names one filesystem may treat as the same get the ID prefix: case and Unicode form", () => {
  const result = python(`
layout = script.Layout("sel")
model = {"entry_id": "E1", "model_id": "E1_m_1"}
names = ["caf\\u00e9.cif", "cafe\\u0301.cif", "CAF\\u00c9.cif"]
ids = ["aaaaaaaa-0000-4000-8000-000000000001", "bbbbbbbb-0000-4000-8000-000000000002", "cccccccc-0000-4000-8000-000000000003"]
print(json.dumps([layout.path({"id": i, "name": n}, model, False) for i, n in zip(ids, names)]))
`);
  assert.deepEqual(result, [
    "dynamic-pdb-sel/E1/E1_m_1/caf\u00e9.cif",
    "dynamic-pdb-sel/E1/E1_m_1/bbbbbbbb_cafe\u0301.cif",
    "dynamic-pdb-sel/E1/E1_m_1/cccccccc_CAF\u00c9.cif",
  ]);
});

test("a path the disk already uses for a file of this run, under another name, is never written over", () => {
  const result = python(`
import os, tempfile
out = tempfile.mkdtemp()
root = os.path.join(out, "dynamic-pdb-sel")
os.makedirs(os.path.join(root, "E1", "E1_m_1"))
layout = script.Layout("sel")
saver = script.Saver(out, root)
layout.taken = saver.taken
model = {"entry_id": "E1", "model_id": "E1_m_1"}
first = layout.path({"id": "aaaaaaaa-0000-4000-8000-000000000001", "name": "model.cif"}, model, False)
with open(saver.local_path(first), "w") as stream:
    stream.write("first\\n")
saver._included(first, "ab" * 32, kept=False)
# An alias the name rules cannot see, as "model.cif." is on Windows: a second name for the same file.
os.link(saver.local_path(first), os.path.join(root, "E1", "E1_m_1", "alias.cif"))
second = layout.path({"id": "bbbbbbbb-0000-4000-8000-000000000002", "name": "alias.cif"}, model, False)
print(json.dumps([first, second]))
`);
  assert.deepEqual(result, ["dynamic-pdb-sel/E1/E1_m_1/model.cif", "dynamic-pdb-sel/E1/E1_m_1/bbbbbbbb_alias.cif"]);
});

test("a path that is already a folder on disk is never a file's: the file gets the ID prefix", () => {
  const result = python(`
import os, tempfile
out = tempfile.mkdtemp()
root = os.path.join(out, "dynamic-pdb-sel")
os.makedirs(os.path.join(root, "E1", "E1_m_1", "notes.cif"))
layout = script.Layout("sel")
saver = script.Saver(out, root)
layout.taken = saver.taken
model = {"entry_id": "E1", "model_id": "E1_m_1"}
print(json.dumps(layout.path({"id": "dddddddd-0000-4000-8000-000000000004", "name": "notes.cif"}, model, False)))
`);
  assert.equal(result, "dynamic-pdb-sel/E1/E1_m_1/dddddddd_notes.cif");
});

test("a folder that resolves outside stops the run even when it is not reported as a link (a Windows junction)", () => {
  const result = python(`
import os, tempfile
out = tempfile.mkdtemp()
outside = tempfile.mkdtemp()
root = os.path.join(out, "dynamic-pdb-sel")
os.makedirs(root)
os.symlink(outside, os.path.join(root, "E1"))
saver = script.Saver(out, root)
# A junction is a folder that resolves elsewhere but is not reported as a symbolic link.
script.os.path.islink = lambda path: False
try:
    saver._folder(["E1", "E1_m_1"], create=True)
    print(json.dumps(None))
except script.ScriptError as error:
    print(json.dumps([str(error), os.listdir(outside)]))
`);
  assert.match(result[0], /E1 leads to .* \(a link or junction\); the script writes only inside/);
  assert.deepEqual(result[1], []);
});

test("a name Windows would read as on a drive stays in its folder", () => {
  const result = python(`
layout = script.Layout("sel")
model = {"entry_id": "E1", "model_id": "E1_m_1"}
print(json.dumps(layout.path({"id": "99999999-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "name": "C:outside.txt"}, model, False)))
`);
  assert.equal(result, "dynamic-pdb-sel/E1/E1_m_1/C_outside.txt");
});

test("a displaced copy is only dropped when .displaced/ holds the very same bytes", () => {
  const result = python(`
import os, tempfile, hashlib
out = tempfile.mkdtemp()
root = os.path.join(out, "dynamic-pdb-sel")
os.makedirs(os.path.join(root, ".displaced"))
saver = script.Saver(out, root)
def write(path, content):
    with open(path, "w") as stream:
        stream.write(content)
digest = hashlib.sha256(b"saved bytes\\n").hexdigest()
# .displaced/<digest> holds other bytes under that name, and a second name is a dangling link
write(os.path.join(root, ".displaced", digest), "altered\\n")
write(os.path.join(root, "a.cif"), "saved bytes\\n")
saver._displace(os.path.join(root, "a.cif"))
kept = open(os.path.join(root, ".displaced", digest)).read()
others = sorted(open(os.path.join(root, ".displaced", name)).read() for name in os.listdir(os.path.join(root, ".displaced")) if name.startswith("other-"))
# the same bytes again: the second copy goes, the kept one stays
write(os.path.join(root, "b.cif"), "saved bytes\\n")
saver._displace(os.path.join(root, "b.cif"))
still = open(os.path.join(root, ".displaced", digest)).read()
# at the end, a copy named like a saved file is dropped only when it holds those bytes
other = hashlib.sha256(b"downloaded\\n").hexdigest()
write(os.path.join(root, ".displaced", other), "altered too\\n")
saver.included = {digest, other}
left = saver.finish()
print(json.dumps({"kept": kept, "others": others, "b": os.path.exists(os.path.join(root, "b.cif")), "still": still,
                  "left": left, "names": sorted(name for name in os.listdir(os.path.join(root, ".displaced")) if not name.startswith("other-")),
                  "other": other}))
`);
  assert.deepEqual(
    { ...result, other: undefined },
    { kept: "saved bytes\n", others: ["altered\n"], b: false, still: "saved bytes\n", left: 2, names: [result.other], other: undefined },
  );
});

test("the script writes nothing through a symbolic link in its folder", async (t) => {
  const server = fakeServer();
  const base = await server.start();
  t.after(() => server.stop());
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-script-"));
  const outside = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-outside-"));
  t.after(() => fs.rmSync(out, { recursive: true, force: true }));
  t.after(() => fs.rmSync(outside, { recursive: true, force: true }));
  const root = path.join(out, `dynamic-pdb-${SELECTION}`);
  fs.mkdirSync(root);

  // a link where a partial download goes is removed, never written through
  const victim = path.join(outside, "victim.txt");
  fs.writeFileSync(victim, "keep me\n");
  const rcsb = server.byID["1ABC.cif"].file;
  fs.symlinkSync(victim, path.join(root, `.incoming-${rcsb.id}.part`));
  fs.symlinkSync(victim, path.join(root, "manifest.tsv.part"));
  const linkedPart = await runScript(scriptArgs(base, out, "original_mmcif"));
  assert.equal(linkedPart.status, 0, linkedPart.stderr);
  assert.equal(fs.readFileSync(victim, "utf8"), "keep me\n");
  assert.equal(fs.existsSync(path.join(root, `.incoming-${rcsb.id}.part`)), false);

  // and a linked folder stops the run before anything is written into it
  fs.rmSync(path.join(root, "E1"), { recursive: true, force: true });
  fs.symlinkSync(outside, path.join(root, "E1"));
  const linkedFolder = await runScript(scriptArgs(base, out, "original_mmcif"));
  assert.equal(linkedFolder.status, 2);
  assert.match(linkedFolder.stderr, /E1 is a symbolic link; the script writes only inside/);
  assert.deepEqual(fs.readdirSync(outside), ["victim.txt"]);

  // and a linked .displaced/ is never taken back from, even when it holds a file's bytes
  const fresh = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-script-"));
  t.after(() => fs.rmSync(fresh, { recursive: true, force: true }));
  const freshRoot = path.join(fresh, `dynamic-pdb-${SELECTION}`);
  fs.mkdirSync(freshRoot);
  const stash = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-stash-"));
  t.after(() => fs.rmSync(stash, { recursive: true, force: true }));
  fs.writeFileSync(path.join(stash, rcsb.recorded_sha256), server.byID["1ABC.cif"].content);
  fs.symlinkSync(stash, path.join(freshRoot, ".displaced"));
  const linkedStash = await runScript(scriptArgs(base, fresh, "original_mmcif"));
  assert.equal(linkedStash.status, 2);
  assert.match(linkedStash.stderr, /\.displaced is a symbolic link/);
  assert.deepEqual(fs.readdirSync(stash), [rcsb.recorded_sha256]);

  // and the selection's folder itself may not be a link
  const linkedRootOut = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-script-"));
  t.after(() => fs.rmSync(linkedRootOut, { recursive: true, force: true }));
  const elsewhere = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-elsewhere-"));
  t.after(() => fs.rmSync(elsewhere, { recursive: true, force: true }));
  fs.symlinkSync(elsewhere, path.join(linkedRootOut, `dynamic-pdb-${SELECTION}`));
  const linkedRoot = await runScript(scriptArgs(base, linkedRootOut, "original_mmcif"));
  assert.equal(linkedRoot.status, 2);
  assert.match(linkedRoot.stderr, /is a symbolic link; choose where the files go with --out instead/);
  assert.deepEqual(fs.readdirSync(elsewhere), []);
});

test("the script asks the API again when an answer ends early", async (t) => {
  const server = fakeServer();
  const base = await server.start();
  t.after(() => server.stop());
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-script-"));
  t.after(() => fs.rmSync(out, { recursive: true, force: true }));
  server.truncate.add(`/api/v1/selections/${SELECTION}`);
  server.truncate.add(`/api/v1/selections/${SELECTION}/models`);

  const result = await runScript(scriptArgs(base, out, "original_mmcif"));

  assert.equal(result.status, 0, result.stderr);
  assert.equal(server.requests.get(`/api/v1/selections/${SELECTION}`), 2);
  assert.equal(readManifest(out).rows.length, 5);
});

test("the original mmCIF and modified coordinates choices hold the archive's files", async (t) => {
  const server = fakeServer();
  const base = await server.start();
  t.after(() => server.stop());
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-script-"));
  t.after(() => fs.rmSync(out, { recursive: true, force: true }));
  const prefix = `dynamic-pdb-${SELECTION}`;

  const original = await runScript(scriptArgs(base, path.join(out, "original"), "original_mmcif"));
  assert.equal(original.status, 0, original.stderr);
  assert.deepEqual(
    readManifest(path.join(out, "original")).rows.map((row) => [row.record_type, row.model_id, row.file_name, row.path_in_archive]),
    [
      ["file", "E1_m_001", "1ABC.cif", `${prefix}/E1/E1_m_001/1ABC.cif`],
      ["no_matching_file", "E1_m_002", "", ""],
      ["no_matching_file", "E2_m_003", "", ""],
      ["file", "E2_m_004", "2XYZ.cif", `${prefix}/E2/E2_m_004/2XYZ.cif`],
      ["no_matching_file", "E3_m_005", "", ""],
    ],
  );

  const modified = await runScript(scriptArgs(base, path.join(out, "modified"), "modified_coordinates"));
  assert.equal(modified.status, 0, modified.stderr);
  const rows = readManifest(path.join(out, "modified")).rows;
  assert.deepEqual(
    rows.map((row) => [row.record_type, row.model_id, row.file_name, row.path_in_archive]),
    [
      ["no_matching_file", "E1_m_001", "", ""],
      ["file", "E1_m_002", "model.cif", `${prefix}/E1/E1_m_002/model.cif`],
      ["file", "E2_m_003", "model.cif", `${prefix}/E2/E2_m_003/model.cif`],
      ["file", "E2_m_003", "model.cif", `${prefix}/E2/E2_m_003/00000011_model.cif`],
      ["no_matching_file", "E2_m_004", "", ""],
      ["file", "E3_m_005", "coords.cif", `${prefix}/E3/E3_m_005/coords.cif`],
    ],
  );
  assert.match(rows[0].reason, /Dynamic PDB coordinate file: it is a PDB archive model/);
});

test("the script waits as long as the file store asks before reading a file again", async (t) => {
  const server = fakeServer();
  const base = await server.start();
  t.after(() => server.stop());
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-script-"));
  t.after(() => fs.rmSync(out, { recursive: true, force: true }));
  const rcsb = server.byID["1ABC.cif"].file;
  server.files.get("/bucket/copies/1ABC.cif").once = { status: 429, headers: { "retry-after": "7" } };

  const result = await runScript(scriptArgs(base, out, "original_mmcif"));

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stderr, /SLEPT \[7\]/);
  const row = readManifest(out).rows.find((candidate) => candidate.file_id === rcsb.id);
  assert.equal(row.status, "included");
});

test("the script reads a file again when its transfer was cut short", async (t) => {
  const server = fakeServer();
  const base = await server.start();
  t.after(() => server.stop());
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-script-"));
  t.after(() => fs.rmSync(out, { recursive: true, force: true }));
  const rcsb = server.byID["1ABC.cif"].file;
  server.files.get("/bucket/copies/1ABC.cif").once = { truncate: true };

  const result = await runScript(scriptArgs(base, out, "original_mmcif"));

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stderr, /SLEPT \[2\]/, "one wait, then a second read");
  const row = readManifest(out).rows.find((candidate) => candidate.file_id === rcsb.id);
  assert.equal(row.status, "included");
  assert.equal(server.requests.get("/bucket/copies/1ABC.cif"), 2);
});

test("the script stops at once on Windows, pointing to WSL and the browser archive", () => {
  const result = spawnSync("python3", ["-I", "-B", "-c", HARNESS, SCRIPT], {
    input: 'script.os.name = "nt"\nsys.exit(script.main(["--selection", "x", "--files", "all_files"]))',
    encoding: "utf8",
  });
  assert.equal(result.status, 2);
  assert.match(result.stderr, /^dynamic-pdb-download: this script runs on macOS and Linux only\. On Windows, run it in WSL/m);
  assert.match(result.stderr, /download the archive from the website's download panel/);
});

test("the script stops with exit status 2 when it cannot run", async (t) => {
  const server = fakeServer();
  const base = await server.start();
  t.after(() => server.stop());
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "dpdb-script-"));
  t.after(() => fs.rmSync(out, { recursive: true, force: true }));

  const off = await runScript(scriptArgs(base, out, "all_files", SELECTION_OFF));
  assert.equal(off.status, 2);
  assert.match(off.stderr, /bulk download is not available on this server yet/);

  const unknown = await runScript(scriptArgs(base, out, "all_files", "00000000-0000-4000-8000-00000000ffff"));
  assert.equal(unknown.status, 2);
  assert.match(unknown.stderr, /no saved selection has the ID/);

  const cut = await runScript(scriptArgs(base, out, "all_files", SELECTION_CUT_404));
  assert.equal(cut.status, 2, cut.stderr);
  assert.match(cut.stderr, /no saved selection has the ID/);
  assert.doesNotMatch(cut.stderr, /Traceback/);

  const mystery = await runScript(scriptArgs(base, out, "modified_coordinates", SELECTION_MYSTERY));
  assert.equal(mystery.status, 2);
  assert.match(mystery.stderr, /unknown model type: 'Mystery'/);

  const notAFolder = path.join(out, "a-file");
  fs.writeFileSync(notAFolder, "");
  const fileAsOut = await runScript(scriptArgs(base, notAFolder, "all_files"));
  assert.equal(fileAsOut.status, 2);
  assert.match(fileAsOut.stderr, /^dynamic-pdb-download: .*a-file/m);
  assert.doesNotMatch(fileAsOut.stderr, /Traceback/);

  const withoutBases = scriptArgs(base, out, "all_files").slice(0, -2);
  assert.deepEqual(withoutBases.slice(-2), ["--api", `${base}/api`]);
  const unknownAPI = await runScript(withoutBases);
  assert.equal(unknownAPI.status, 2);
  assert.match(unknownAPI.stderr, /is not a Dynamic PDB deployment this script knows .*--storage-base/);
  assert.doesNotMatch(unknownAPI.stderr, /Traceback/);

  const notAnID = spawnSync("python3", ["-I", "-B", SCRIPT, "--selection", "abc", "--files", "all_files"], { encoding: "utf8" });
  assert.equal(notAnID.status, 2);
  assert.match(notAnID.stderr, /--selection must be a selection ID/);
});

// Last, after every run above: the website serves this folder as it is.
test("the tests leave no bytecode in the folder the website serves", () => {
  assert.deepEqual(fs.readdirSync(path.dirname(SCRIPT)), ["dynamic-pdb-download.py"]);
});
