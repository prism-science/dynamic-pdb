require("./register.cjs");

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const { createEntry } = require("../src/lib/api/entries.ts");
const { mergeFileMetadata } = require("../src/app/components/entry-form/autoDetect.ts");
const { parseStructureFacts } = require("../src/lib/parse/structure.ts");

const jsonApiMediaType = "application/vnd.api+json";

function fixture(name) {
  return fs.readFileSync(
    path.resolve(__dirname, "..", "..", "lib", "cif", "testdata", name),
    "utf8",
  );
}

// Sends one model whose coordinate file was parsed by the preview, and returns
// the model data of the request the backend would receive.
async function createdModelData(fileMetadata, modelMetadata) {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    let body = null;
    global.fetch = async (_url, init) => {
      body = JSON.parse(init.body);
      return new Response(
        JSON.stringify({
          data: {
            type: "entry_revision_results",
            id: "entry-revision-1",
            attributes: {
              entry_id: "entry-1",
              revision_id: "entry-revision-1",
              state: "in_review",
              model_results: [],
            },
          },
        }),
        { status: 201, headers: { "content-type": jsonApiMediaType } },
      );
    };
    await createEntry("token-123", {
      title: "Entry",
      method: "X-ray crystallography",
      models: [
        {
          title: "Model",
          ...(modelMetadata ? { metadata: modelMetadata } : {}),
          entities: [
            {
              id: "model-artifact",
              type: "model",
              level: "L2",
              name: "4hhb.cif",
              payload: { file_url: "s3://dynamic-pdb/4hhb.cif", metadata: fileMetadata },
            },
          ],
        },
      ],
    });
    return body.model_operations[0].data;
  } finally {
    global.fetch = previousFetch;
    if (previousApiBaseURL === undefined) {
      delete process.env.NEXT_PUBLIC_API_BASE_URL;
    } else {
      process.env.NEXT_PUBLIC_API_BASE_URL = previousApiBaseURL;
    }
  }
}

test("a website upload sends the compound review flags under extensions.compound_review", async () => {
  // given
  const facts = parseStructureFacts(fixture("4HHB.cif"), "mmcif");
  const fileMetadata = mergeFileMetadata(facts.metadata, undefined);

  // when
  const data = await createdModelData(fileMetadata);

  // then
  assert.deepEqual(data.metadata.cofactors, ["HEM"]);
  assert.deepEqual(data.metadata.ligands, ["PO4"]);
  assert.deepEqual(data.metadata.extensions, {
    compound_review: [
      {
        list: "ligands",
        id: "PO4",
        reason:
          "compound is on the draft solvent list, which researchers have not approved: kept as a ligand instead of dropped",
      },
    ],
  });
});

test("the flags the website sends are the ones the CLI sends for the same file", async () => {
  // given
  const golden = JSON.parse(
    fs.readFileSync(
      path.resolve(__dirname, "..", "..", "lib", "cif", "testdata", "parity-golden.json"),
      "utf8",
    ),
  );
  const facts = parseStructureFacts(fixture("4HHB.cif"), "mmcif");

  // when
  const data = await createdModelData(mergeFileMetadata(facts.metadata, undefined));

  // then
  const expected = golden["4HHB.cif"].compounds
    .filter((compound) => compound.needs_review)
    .map(({ list, id, reason }) => ({ list, id, reason }));
  assert.deepEqual(data.metadata.extensions.compound_review, expected);
});

test("review flags are merged with the extensions the form already sets", async () => {
  // given
  const facts = parseStructureFacts(fixture("4HHB.cif"), "mmcif");
  const fileMetadata = mergeFileMetadata(facts.metadata, undefined);

  // when
  const data = await createdModelData(fileMetadata, {
    extensions: { campaign: "round 2" },
  });

  // then
  assert.equal(data.metadata.extensions.campaign, "round 2");
  assert.equal(data.metadata.extensions.compound_review.length, 1);
});

test("a file without flagged compounds sends no extensions", async () => {
  // given
  const facts = parseStructureFacts(fixture("3CLN.cif"), "mmcif");

  // when
  const data = await createdModelData(mergeFileMetadata(facts.metadata, undefined));

  // then
  assert.deepEqual(data.metadata.cofactors, ["CA"]);
  assert.equal("extensions" in data.metadata, false);
});

test("mergeFileMetadata keeps what the form already holds and adds the flags next to it", () => {
  // given
  const found = { atom_count: 10, extensions: { compound_review: [{ list: "ligands", id: "PO4", reason: "r" }] } };
  const existing = { atom_count: 99, extensions: { lab_note: "kept" } };

  // when
  const merged = mergeFileMetadata(found, existing);

  // then
  assert.equal(merged.atom_count, 99);
  assert.deepEqual(merged.extensions, {
    compound_review: [{ list: "ligands", id: "PO4", reason: "r" }],
    lab_note: "kept",
  });
});
