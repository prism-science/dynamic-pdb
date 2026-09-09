require("./register.cjs");

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  parseStructureFacts,
  detectStructureFormat,
} = require("@/lib/parse/structure");
const { parseRunLog, looksLikeRunLog } = require("@/lib/parse/runLog");

// Column positions are the whole game in the PDB format, so the fixture is
// built from field widths rather than typed by eye.
function atomLine({
  record = "ATOM  ",
  serial = 1,
  name = " N  ",
  resName = "MET",
  altLoc = " ",
  chain = "A",
  resSeq = 1,
  element = "N",
}) {
  return (
    record +
    String(serial).padStart(5) +
    " " +
    name.padEnd(4) +
    altLoc +
    resName.padStart(3) +
    " " +
    chain +
    String(resSeq).padStart(4) +
    " " +
    "   " +
    "  20.154" +
    "  29.699" +
    "   5.276" +
    "  1.00" +
    " 49.05" +
    " ".repeat(10) +
    element.padStart(2)
  );
}

const PDB = [
  "HEADER    HYDROLASE                               01-JUN-16   5GY3",
  "EXPDTA    X-RAY DIFFRACTION",
  "SOURCE   2 ORGANISM_SCIENTIFIC: KLEBSIELLA PNEUMONIAE;",
  "AUTHOR    A.ATTIGANI,S.P.LI,",
  "AUTHOR   2 L.F.SUN,J.VAN DER BERG",
  "REMARK 200  RESOLUTION RANGE HIGH      (A) : 1.770",
  "REMARK   3  REFINEMENT.",
  "REMARK   3   PROGRAM     : PHENIX (1.21.2_5108)",
  "REMARK   3   R VALUE            (WORKING SET) : 0.163",
  "REMARK   3   FREE R VALUE                     : 0.195",
  "REMARK   3   BIN R VALUE       (WORKING SET) : 0.234",
  "REMARK   3   BIN FREE R VALUE                : 0.279",
  "REMARK   3   RESOLUTION RANGE HIGH (ANGSTROMS) : 1.77",
  "CRYST1   45.120   67.330   98.410  90.00  90.00  90.00 P 21 21 21    4",
  atomLine({ serial: 1, resSeq: 1, chain: "A" }),
  atomLine({
    serial: 2,
    name: " CA ",
    resSeq: 1,
    chain: "A",
    element: "C",
    altLoc: "A",
  }),
  atomLine({ serial: 3, name: " N  ", resSeq: 2, chain: "A" }),
  atomLine({ serial: 4, name: " H  ", resSeq: 2, chain: "A", element: "H" }),
  atomLine({
    record: "HETATM",
    serial: 5,
    name: " C1 ",
    resName: "NAG",
    chain: "A",
    resSeq: 401,
    element: "C",
  }),
  atomLine({
    record: "HETATM",
    serial: 6,
    name: " O  ",
    resName: "HOH",
    chain: "A",
    resSeq: 501,
    element: "O",
  }),
  "END",
].join("\n");

test("pdb header yields program, metrics and composition", () => {
  const facts = parseStructureFacts(PDB, "pdb");

  assert.deepEqual(facts.program, { name: "PHENIX", version: "1.21.2_5108" });
  assert.equal(facts.metrics.r_work, 0.163);
  assert.equal(facts.metrics.r_free, 0.195);
  assert.equal(facts.metadata.resolution, 1.77);
  assert.equal(facts.metadata.space_group, "P 21 21 21");
  // Mapped onto the value the record stores, not the one the header shouts.
  assert.equal(facts.metadata.method, "X-ray crystallography");
  assert.equal(facts.pdbId, "5GY3");
  // Four heavy atoms: hydrogen and water are excluded, the sugar is not.
  assert.equal(facts.metadata.atom_count, 4);
  assert.equal(facts.metadata.modeled_residues, 2);
  assert.equal(facts.metadata.unique_protein_chains, 1);
  assert.equal(facts.metadata.altloc_fraction, 0.5);
  assert.deepEqual(facts.metadata.ligands, ["NAG"]);
  // Shouted `A.ATTIGANI` becomes the `Surname, I.N.` form the pages display,
  // and a continuation line continues the same comma-separated list.
  assert.deepEqual(facts.authors, [
    "Attigani, A.",
    "Li, S.P.",
    "Sun, L.F.",
    "Van Der Berg, J.",
  ]);
});

const MMCIF = [
  "data_5GY3",
  "#",
  "_exptl.method            'X-RAY DIFFRACTION'",
  "_symmetry.space_group_name_H-M   'P 21 21 21'",
  "_refine.ls_d_res_high            1.7700",
  "_refine.ls_R_factor_R_work       0.1630",
  "_refine.ls_R_factor_R_free       0.1950",
  "_entity_src_gen.pdbx_gene_src_scientific_name   'Klebsiella pneumoniae'",
  "#",
  "loop_",
  "_audit_author.name",
  "_audit_author.pdbx_ordinal",
  "'Attigani, A.'  1",
  "'Li, S.P.'      2",
  "#",
  "loop_",
  "_software.name",
  "_software.version",
  "_software.classification",
  "XDS        .        'data reduction'",
  "PHENIX     1.21.2   refinement",
  "#",
  "loop_",
  "_atom_site.group_PDB",
  "_atom_site.id",
  "_atom_site.type_symbol",
  "_atom_site.label_comp_id",
  "_atom_site.auth_asym_id",
  "_atom_site.auth_seq_id",
  "_atom_site.label_alt_id",
  "ATOM   1 N MET A 1 .",
  "ATOM   2 C MET A 1 A",
  "ATOM   3 N ALA A 2 .",
  "HETATM 4 C NAG A 401 .",
  "HETATM 5 O HOH A 501 .",
  "#",
].join("\n");

test("mmcif picks the refinement software, not the first one listed", () => {
  const facts = parseStructureFacts(MMCIF, "mmcif");

  assert.deepEqual(facts.program, { name: "PHENIX", version: "1.21.2" });
  assert.equal(facts.metrics.r_work, 0.163);
  assert.equal(facts.metrics.r_free, 0.195);
  assert.equal(facts.metadata.resolution, 1.77);
  assert.equal(facts.metadata.space_group, "P 21 21 21");
  assert.equal(facts.metadata.atom_count, 4);
  assert.equal(facts.metadata.modeled_residues, 2);
  assert.equal(facts.metadata.unique_protein_chains, 1);
  assert.equal(facts.metadata.altloc_fraction, 0.5);
  assert.deepEqual(facts.metadata.ligands, ["NAG"]);
  // mmCIF already stores the display form, so it is taken verbatim.
  assert.deepEqual(facts.authors, ["Attigani, A.", "Li, S.P."]);
});

test("the phenix build tag is not mistaken for the version", () => {
  // Real re-refined output: phenix states its release and an unknown build.
  const header = [
    "REMARK   3 REFINEMENT.",
    "REMARK   3   PROGRAM     : PHENIX (2.0_5824: ???)",
    // The developers of phenix, not the depositors. Only a first-column
    // AUTHOR record names those, and these files carry none.
    "REMARK   3   AUTHORS     : Adams,Afonine,Bunkoczi,Burnley,Chen,",
    "REMARK   3               : Sauter,Sobolev,Storoni,Terwilliger,Zwart",
    "REMARK   3   R VALUE            (WORKING SET) : 0.1767",
  ].join("\n");

  const facts = parseStructureFacts(header, "pdb");

  assert.deepEqual(facts.program, { name: "PHENIX", version: "2.0_5824" });
  assert.deepEqual(facts.authors, []);
});

test("format is taken from the extension", () => {
  assert.equal(detectStructureFormat("refine_001.pdb"), "pdb");
  assert.equal(detectStructureFormat("5gy3.cif"), "mmcif");
  assert.equal(detectStructureFormat("data.mtz"), null);
});

const PHENIX_EFF = [
  "phenix.refine 1.21.2",
  "refinement {",
  "  input {",
  "    pdb {",
  '      file_name = "/home/xtal/5gy3/start.pdb"',
  "    }",
  "    xray_data {",
  '      file_name = "/home/xtal/5gy3/5gy3_sf.mtz"',
  "    }",
  "  }",
  "  output {",
  '    prefix = "refine"',
  "    serial = 1",
  "  }",
  "}",
].join("\n");

test("phenix parameters give inputs and a derived output stem", () => {
  const facts = parseRunLog(PHENIX_EFF);

  assert.ok(facts);
  assert.equal(facts.program.name, "phenix.refine");
  assert.equal(facts.program.version, "1.21.2");
  assert.deepEqual(facts.inputs.sort(), ["5gy3_sf.mtz", "start.pdb"]);
  assert.ok(facts.outputs.includes("refine_001.pdb"));
});

const REFMAC_LOG = [
 " ###  Refmac_5.8.0258  ###",
  " XYZIN  /data/5gy3/start.pdb",
  " HKLIN  /data/5gy3/5gy3_sf.mtz",
  " XYZOUT /data/5gy3/refmac_out.pdb",
  " HKLOUT /data/5gy3/refmac_out.mtz",
].join("\n");

test("refmac logical names become inputs and outputs", () => {
  const facts = parseRunLog(REFMAC_LOG);

  assert.ok(facts);
  assert.equal(facts.program.name, "refmac5");
  assert.equal(facts.program.version, "5.8.0258");
  assert.deepEqual(facts.inputs.sort(), ["5gy3_sf.mtz", "start.pdb"]);
  assert.deepEqual(facts.outputs.sort(), ["refmac_out.mtz", "refmac_out.pdb"]);
});

test("a log naming no files is treated as no log at all", () => {
  assert.equal(parseRunLog("phenix.refine 1.21.2 finished normally"), null);
  assert.equal(parseRunLog("nothing recognisable here"), null);
  assert.equal(looksLikeRunLog("refine_001.log"), true);
  assert.equal(looksLikeRunLog("model.pdb"), false);
});

const { missingExpectedInputs } = require("@/lib/../app/components/entry-form/helpers");
const { buildDraftGraph } = require("@/lib/../app/components/entry-form/draftLineage");

const program = (patch) => ({
  id: "p1",
  name: "phenix.refine",
  version: "1.21.2",
  description: "",
  inputFileIds: [],
  outputFileIds: [],
  expectedInputs: [],
  origin: "manual",
  ...patch,
});

const file = (patch) => ({
  id: "f1",
  source: "upload",
  name: "refine_001.pdb",
  size: 10,
  type: "pdb",
  level: "L2",
  authors: "",
  url: "s3://x",
  progress: 1,
  uploadStatus: "uploaded",
  uploadError: null,
  ...patch,
});

test("declared inputs nobody uploaded are reported once, with their source", () => {
  const missing = missingExpectedInputs([
    program({
      expectedInputs: [
        { id: "e1", name: "5gy3_free.mtz", source: "refine_001.eff" },
      ],
    }),
    program({ id: "p2", name: "qFit" }),
  ]);

  assert.deepEqual(missing, [
    {
      program: "phenix.refine",
      name: "5gy3_free.mtz",
      source: "refine_001.eff",
    },
  ]);
});

test("draft graph places placeholders in the chain and strays in the tray", () => {
  const model = file({ id: "model-1" });
  const map = file({ id: "map-1", name: "map.mtz", type: "mtz", level: "L1" });
  const stray = file({ id: "stray-1", name: "notes.txt", type: "unknown", level: "L0" });

  const graph = buildDraftGraph({
    id: "m",
    name: "",
    description: "",
    thumbFileId: "t",
    thumbFile: null,
    thumbPreview: null,
    thumbUrl: null,
    thumbProgress: 0,
    thumbUploadStatus: "idle",
    thumbUploadError: null,
    files: [model, map, stray],
    metrics: [],
    programs: [
      program({
        inputFileIds: ["map-1"],
        outputFileIds: ["model-1"],
        expectedInputs: [
          { id: "e1", name: "5gy3_free.mtz", source: "refine_001.eff" },
        ],
      }),
    ],
  });

  const kinds = graph.lineage.nodes.map((node) => [node.name, node.kind, node.generation]);
  assert.deepEqual(kinds, [
    ["5gy3_free.mtz", "expected", 0],
    ["map.mtz", "artifact", 0],
    ["phenix.refine", "run", 1],
    ["refine_001.pdb", "artifact", 2],
  ]);
  assert.deepEqual(
    graph.unlinkedFiles.map((f) => f.name),
    ["notes.txt"],
  );
});

const { collapseWideRows, MAX_ROW_NODES } = require("@/lib/lineage");

test("a wide fan-out collapses into one counted node", () => {
  const run = {
    id: "run", kind: "run", name: "qFit", version: "4.0.0", level: null,
    typeLabel: null, entity: null, generation: 0, onPath: true, current: false,
  };
  const outputs = Array.from({ length: 12 }, (_, i) => ({
    id: `out-${i}`, kind: "artifact", name: `member_${i}.pdb`, version: null,
    level: "L2", typeLabel: "model", entity: null, generation: 1,
    // Only the first is the model this deposit is about.
    onPath: i === 0, current: i === 0,
  }));

  const collapsed = collapseWideRows({
    nodes: [run, ...outputs],
    edges: outputs.map((o) => ({ id: `run->${o.id}`, source: "run", target: o.id, onPath: o.onPath })),
    runCount: 1,
  });

  const row = collapsed.nodes.filter((n) => n.generation === 1);
  assert.equal(row.length, MAX_ROW_NODES);
  // The chain's own model survives; the rest become a count.
  assert.ok(row.some((n) => n.current));
  const overflow = row.find((n) => n.kind === "overflow");
  assert.equal(overflow.hiddenCount, 12 - (MAX_ROW_NODES - 1));
  // The run still visibly produces something rather than trailing into nothing.
  assert.ok(collapsed.edges.some((e) => e.target === overflow.id));
  assert.equal(collapsed.edges.length, MAX_ROW_NODES);
});

test("a row that already fits is left alone", () => {
  const lineage = {
    nodes: [
      { id: "a", kind: "artifact", name: "a", version: null, level: "L1", typeLabel: "data", entity: null, generation: 0, onPath: true, current: false },
      { id: "b", kind: "artifact", name: "b", version: null, level: "L1", typeLabel: "data", entity: null, generation: 0, onPath: true, current: false },
    ],
    edges: [],
    runCount: 0,
  };
  assert.equal(collapseWideRows(lineage), lineage);
});
