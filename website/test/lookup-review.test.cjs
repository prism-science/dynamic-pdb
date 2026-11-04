require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  UNCLEAR_MUTATIONS_ADVICE,
  lookupReviewMessages,
} = require("../src/app/components/entry-form/lookupReview.ts");
const {
  mutationsNeedingReview,
  polymerEntityDraftsFromArchive,
} = require("../src/app/components/entry-form/polymerEntities.ts");
const { entryFormMessages } = require("../src/app/components/entry-form/validation.ts");
const { emptyEntryMetadataDraft } = require("../src/app/components/entry-form/types.ts");

// The New entry form shows what the PDB lookup says about its own values
// (conflicts and warnings) beside the field each one is about, and flags
// mutations it cannot check.

test("should show a conflict beside the field RCSB reports more than one value for", () => {
  // given
  const review = lookupReview({
    entry: entryProposal({
      conflicts: [
        { field: "method", chosen: "X-RAY DIFFRACTION", others: ["NEUTRON DIFFRACTION"] },
      ],
    }),
  });

  // when
  const messages = lookupReviewMessages("7L0B", review);

  // then
  assert.deepEqual(messages, [
    {
      field: "entry.method",
      item: null,
      message:
        "PDB 7L0B lists more than one method: X-RAY DIFFRACTION, NEUTRON DIFFRACTION. The form fills in the first",
      blocking: false,
    },
  ]);
});

test("should put a warning about a field the form does not show beside the PDB ID", () => {
  // given
  const review = lookupReview({
    entry: entryProposal({
      warnings: [{ source: "archive", field: "status_code", detail: 'RCSB status is "HPUB", not released' }],
    }),
    warnings: [{ source: "archive", field: "uniprot_release", detail: "SIFTS reports no UniProt release for 7L0B" }],
  });

  // when
  const messages = lookupReviewMessages("7L0B", review);

  // then
  assert.deepEqual(
    messages.map((message) => [message.field, message.message, message.blocking]),
    [
      ["entry.pdb", "PDB 7L0B: SIFTS reports no UniProt release for 7L0B", false],
      ["entry.pdb", 'PDB 7L0B: RCSB status is "HPUB", not released', false],
    ],
  );
});

test("should show a value that breaks a schema rule beside the field its pointer names", () => {
  // given
  const review = lookupReview({
    entry: entryProposal({
      warnings: [
        { source: "validation", pointer: "/resolution", code: "out_of_range", detail: "must be greater than 0" },
      ],
    }),
  });

  // when
  const [message] = lookupReviewMessages("7L0B", review);

  // then
  assert.equal(message.field, "entry.resolution");
  assert.equal(message.message, "the value from PDB 7L0B breaks a schema rule: must be greater than 0");
  assert.equal(message.blocking, false);
});

test("should show an entity's warnings on that entity, and skip an entity without a label", () => {
  // given
  const review = lookupReview({
    polymerEntities: [
      entityProposal({
        label_entity_id: "2",
        warnings: [
          { source: "archive", field: "alignment_mappings", detail: 'UniProt release is unknown for alignment mapping "P00698"' },
          { source: "archive", field: "something_new", detail: "a field the form does not know" },
        ],
      }),
      entityProposal({
        warnings: [{ source: "archive", field: "construct", detail: "not shown: no card" }],
      }),
    ],
  });

  // when
  const messages = lookupReviewMessages("7L0B", review);

  // then
  assert.deepEqual(
    messages.map((message) => [message.field, message.item]),
    [
      ["entity.uniprot", "2"],
      ["entity.block", "2"],
    ],
  );
});

test("should flag archive mutation text that is not single substitutions", () => {
  // given
  const proposal = entityProposal({
    label_entity_id: "1",
    mutations: ["C54T"],
    mutations_raw: "C54T, ENGINEERED MUTATION",
    mutations_status: "needs_review",
  });

  // when
  const messages = lookupReviewMessages("7L0B", lookupReview({ polymerEntities: [proposal] }));
  const [draft] = polymerEntityDraftsFromArchive([proposal], []);

  // then
  assert.deepEqual(messages, [
    { field: "entity.mutations", item: "1", message: UNCLEAR_MUTATIONS_ADVICE, blocking: false },
  ]);
  assert.equal(draft.archiveMutationsRaw, "C54T, ENGINEERED MUTATION");
  assert.deepEqual(draft.archiveMutations, ["C54T"]);
});

test("should read every mutation status and refuse an unknown one", () => {
  // given / when / then
  assert.equal(mutationsNeedingReview({ mutations_raw: " A(-3)G ", mutations_status: "needs_review" }), "A(-3)G");
  assert.equal(mutationsNeedingReview({ mutations_raw: "C54T", mutations_status: "parsed" }), "");
  assert.equal(mutationsNeedingReview({ mutations_raw: "NO", mutations_status: "none" }), "");
  assert.equal(mutationsNeedingReview({}), "");
  assert.throws(
    () => mutationsNeedingReview({ mutations_status: "guessed" }),
    /unknown mutations status: guessed/,
  );
});

test("should refuse a warning source it does not know", () => {
  // given
  const review = lookupReview({
    entry: entryProposal({ warnings: [{ source: "sifts", field: "title", detail: "?" }] }),
  });

  // when / then
  assert.throws(() => lookupReviewMessages("7L0B", review), /unknown archive warning source: sifts/);
});

test("should advise review of typed mutations on an entity the PDB maps to no UniProt entry", () => {
  // given
  const metadata = {
    ...emptyEntryMetadataDraft(),
    method: "X-ray crystallography",
    polymerEntities: [
      entityDraft({ labelEntityId: "1", mutations: ["C54T"], uniprotAccessions: [] }),
      entityDraft({ labelEntityId: "2", mutations: ["C54T"], uniprotAccessions: ["P00698"] }),
      entityDraft({ labelEntityId: "3", mutations: [], uniprotAccessions: [] }),
    ],
  };

  // when
  const messages = entryFormMessages("Title", metadata).filter(
    (message) => message.field === "entity.mutations",
  );

  // then
  assert.deepEqual(messages, [
    {
      field: "entity.mutations",
      item: "1",
      message:
        "needs review: the PDB maps this entity to no UniProt entry, so these positions cannot be checked against UniProt numbering",
      blocking: false,
    },
  ]);
});

function lookupReview(overrides) {
  return {
    entry: entryProposal({}),
    polymerEntities: [],
    warnings: [],
    ...overrides,
  };
}

function entryProposal(overrides) {
  return {
    provenance: {},
    conflicts: [],
    warnings: [],
    revision_groups: [],
    supersedes: [],
    superseded_by: [],
    ...overrides,
  };
}

function entityProposal(overrides) {
  return {
    provenance: {},
    conflicts: [],
    warnings: [],
    source_organisms: [],
    uniprot_mappings: [],
    ...overrides,
  };
}

function entityDraft(overrides) {
  return {
    labelEntityId: "1",
    description: "",
    uniprotAccessions: [],
    archiveOrganisms: ["Gallus gallus"],
    archiveConstruct: "",
    archiveMutations: [],
    archiveMutationsRaw: "",
    construct: "",
    mutations: [],
    organismName: "",
    organismTaxonomyId: "",
    ...overrides,
  };
}
