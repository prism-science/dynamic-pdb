const entryBase = {
  title: "Lysozyme at 100 K",
  method: "x-ray",
  resolution: 1.5,
  space_group: "P 21 21 21",
  external_refs: [{ source: "pdb", id: "1ABC" }],
};

const crystal = {
  crystal_id: "crystal-1",
  growth: { method: "vapor diffusion", temperature_k: 293 },
  diffraction: { temperature_k: 100, wavelength_angstrom: 0.98 },
};

const coordinateEntity = {
  id: "00000000-0000-4000-8000-000000000001",
  type: "model",
  artifact_type: "structure",
  level: "L1",
  name: "model.cif",
  payload: {
    file_url: "https://files.example/model.cif",
    sha256: "a".repeat(64),
    size: 1024,
    type: "predicted",
    metadata: { atom_count: 1200, ligands: ["HEM"], note: "kept", __reserved: 1 },
  },
};

const extEntity = {
  id: "00000000-0000-4000-8000-000000000002",
  type: "model",
  artifact_type: "structure",
  level: "L2",
  name: "ext.cif",
  payload: {
    file_url: "https://files.example/ext.cif",
    metadata: {
      _dynamic_pdb_source: { provider: "ext", id: "x1" },
      extensions: { keep: true },
      atom_count: 5,
      custom: { nested: 1, __hidden: 2 },
    },
  },
};

const dataEntity = {
  id: "00000000-0000-4000-8000-000000000003",
  type: "data",
  artifact_type: "reflections",
  level: "L0",
  name: "data.mtz",
  payload: { file_url: "https://files.example/data.mtz", size: 99 },
};

const programEntity = {
  id: "00000000-0000-4000-8000-000000000004",
  type: "program",
  name: "Phenix",
  payload: { name: "phenix.refine", version: "1.21", description: "refinement" },
};

const metricsEntity = {
  id: "00000000-0000-4000-8000-000000000005",
  type: "metrics",
  name: "Metrics",
  payload: {
    r_work: 0.18,
    r_free: 0.22,
    clashscore: 3.5,
    wilson_b: 21.3,
    rmsf: Number.NaN,
  },
};

const relations = [
  {
    source_entity_id: dataEntity.id,
    target_entity_id: programEntity.id,
    relation_type: "input_to",
  },
  {
    source_entity_id: coordinateEntity.id,
    target_entity_id: programEntity.id,
    relation_type: "output_of",
  },
];

const modelInput = {
  title: "Refined model",
  thumbnail_image_url: "https://files.example/thumb.png",
  metadata: { purpose: "refinement" },
  entities: [coordinateEntity, extEntity, dataEntity, programEntity, metricsEntity],
  relations,
};

const inputs = {
  entry_without_crystals: { ...entryBase },
  entry_with_crystals: {
    ...entryBase,
    crystallography: { crystals: [crystal] },
    entities: [dataEntity],
    models: [modelInput],
  },
  entry_with_two_models: {
    ...entryBase,
    models: [modelInput, { title: "Bare model" }],
  },
  model_with_metrics_runs_artifacts: modelInput,
};

function jsonApiResource(type, attributes) {
  return JSON.stringify({ data: { type, id: "id-1", attributes } });
}

async function captureRequests(entries) {
  const previousFetch = global.fetch;
  const previousBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
  const captured = {};
  let current = "";
  global.fetch = async (url, init) => {
    captured[current] = { url: String(url), method: init.method, body: init.body };
    return new Response(
      jsonApiResource("results", {
        entry_id: "e",
        model_id: "m",
        revision_id: "r",
        state: "in_review",
      }),
      { status: 201, headers: { "Content-Type": "application/vnd.api+json" } },
    );
  };
  try {
    for (const [name, input] of Object.entries(inputs)) {
      current = name;
      if (name.startsWith("model_")) {
        await entries.createModel("token", "entry-1", input);
      } else {
        await entries.createEntry("token", input);
      }
    }
    current = "revision_submit";
    await entries.submitModelRevision("token", "user-1", "entry-1", "model-1", "rev-1");
  } finally {
    global.fetch = previousFetch;
    if (previousBaseURL === undefined) {
      delete process.env.NEXT_PUBLIC_API_BASE_URL;
    } else {
      process.env.NEXT_PUBLIC_API_BASE_URL = previousBaseURL;
    }
  }
  return captured;
}

module.exports = { captureRequests };
