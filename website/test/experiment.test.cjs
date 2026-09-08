require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { experimentView } = require("../src/lib/experiment.ts");
const { crystallographyView } = require("../src/lib/crystallography.ts");

// The header of PDB 5NX1, copied out of the deposited file. Values recorded as
// "?" are left as they are: unknown items are the common case, and the reader
// has to drop their rows rather than print them.
const CIF = `data_5NX1
_cell.angle_alpha                  90.00
_cell.angle_beta                   90.00
_cell.angle_gamma                  90.00
_cell.entry_id                     5NX1
_cell.length_a                     59.543
_cell.length_b                     77.702
_cell.length_c                     92.207
_cell.Z_PDB                        4
_symmetry.entry_id                 5NX1
_symmetry.space_group_name_H-M     'P 21 21 21'
_exptl.entry_id                    5NX1
_exptl.crystals_number             1
_exptl.method                      'X-RAY DIFFRACTION'
_exptl_crystal.density_Matthews    2.49
_exptl_crystal.density_percent_sol 50.57
_exptl_crystal_grow.crystal_id     1
_exptl_crystal_grow.method         'VAPOR DIFFUSION'
_exptl_crystal_grow.pH             ?
_exptl_crystal_grow.temp           293
_exptl_crystal_grow.pdbx_details   '0.2M Ammonium Sulfate, 0.1M Bis-Tris pH 5.5, 25% Polyethylene Glycol 3350'
_diffrn.ambient_temp               100
_diffrn.crystal_id                 1
_diffrn.id                         1
_diffrn_detector.detector          PIXEL
_diffrn_detector.type              'DECTRIS PILATUS3 6M'
_diffrn_detector.pdbx_collection_date 2016-07-22
_diffrn_radiation.pdbx_monochromatic_or_laue_m_l M
_diffrn_radiation.pdbx_diffrn_protocol 'SINGLE WAVELENGTH'
_diffrn_radiation.pdbx_scattering_type x-ray
_diffrn_radiation_wavelength.id    1
_diffrn_radiation_wavelength.wavelength 0.969
_diffrn_source.source              SYNCHROTRON
_diffrn_source.type                'ESRF BEAMLINE ID30B'
_diffrn_source.pdbx_synchrotron_beamline ID30B
_diffrn_source.pdbx_synchrotron_site ESRF
_reflns.d_resolution_high          1.853
_reflns.d_resolution_low           47.262
_reflns.number_obs                 36867
_reflns.percent_possible_obs       99.52
_reflns.pdbx_redundancy            2.0
_reflns.pdbx_netI_over_sigmaI      13.48
_reflns.pdbx_Rpim_I_all            0.031
_reflns_shell.d_res_high           1.853
_reflns_shell.d_res_low            1.92
_reflns_shell.meanI_over_sigI_obs  1.08
_reflns_shell.number_unique_all    3528
_reflns_shell.percent_possible_all 96.37
_reflns_shell.pdbx_redundancy      2.0
_reflns_shell.pdbx_Rpim_I_all      0.642
_reflns_shell.pdbx_CC_half         0.574
_refine.entry_id                   5NX1
_refine.ls_d_res_high              1.853
_refine.ls_d_res_low               47.262
_refine.ls_number_reflns_obs       36853
_refine.ls_number_reflns_R_free    1848
_refine.ls_percent_reflns_obs      99.42
_refine.ls_percent_reflns_R_free   5.01
_refine.ls_R_factor_obs            0.1860
_refine.ls_R_factor_R_free         0.2255
_refine.ls_R_factor_R_work         0.1839
_refine.B_iso_mean                 ?
#
loop_
_software.classification
_software.name
_software.version
_software.pdbx_ordinal
refinement       PHENIX '(1.11.1_2575: ???)' 1
'data reduction' XDS    .                    2
'data scaling'   XDS    .                    3
phasing          PHASER .                    4
#
`;

const ENTRY = {
  id: "e1", created_by: "u1", title: "Cellobiohydrolase I",
  thumbnail_image_url: null, created_at: "", updated_at: "",
};

function block(view, key) {
  return view.blocks.find((item) => item.key === key);
}

function facts(view, key) {
  return Object.fromEntries(
    block(view, key).facts.map((item) => [item.label, item.value]),
  );
}

function rows(view, key) {
  return block(view, key).rows.map((row) => row.cells);
}

test("should lay the tab out in RCSB's order, under RCSB's headings", () => {
  // when
  const view = experimentView(ENTRY, null, CIF);

  // then
  assert.deepEqual(
    view.blocks.map((item) => [item.key, item.title]),
    [
      ["snapshot", "Experimental data snapshot"],
      ["crystallization", "Crystallization"],
      ["properties", "Crystal properties"],
      ["cell", "Crystal data"],
      ["diffraction", "Diffraction experiment"],
      ["source", "Radiation source"],
      ["collection", "Data collection"],
      ["refinement", "Refinement statistics"],
      ["software", "Software"],
    ],
  );
});

test("should read the snapshot, the crystal properties and the cell", () => {
  // when
  const view = experimentView(ENTRY, null, CIF);

  // then
  assert.deepEqual(facts(view, "snapshot"), {
    Method: "X-ray crystallography",
    Resolution: "1.85 Å",
    "R-value free": "0.226",
    "R-value work": "0.184",
    "R-value observed": "0.186",
  });
  assert.deepEqual(facts(view, "properties"), {
    "Matthews coefficient": "2.49 Å³/Da",
    "Solvent content": "50.6%",
  });
  assert.deepEqual(facts(view, "cell"), {
    "Space group": "P 21 21 21",
    "Unit cell lengths": "59.54, 77.70, 92.21 Å",
    "Unit cell angles": "90.00, 90.00, 90.00°",
    Z: "4",
  });
});

test("should read the crystallization, the diffraction and its source", () => {
  // when
  const view = experimentView(ENTRY, null, CIF);

  // then: pH is recorded as "?" in this deposit, so it has no row at all
  assert.deepEqual(facts(view, "crystallization"), {
    Method: "Vapor diffusion",
    Temperature: "293 K",
    Details:
      "0.2M Ammonium Sulfate, 0.1M Bis-Tris pH 5.5, 25% Polyethylene Glycol 3350",
  });
  assert.deepEqual(facts(view, "diffraction"), {
    "Scattering type": "X-ray",
    "Collection temperature": "100 K",
    Detector: "Pixel",
    "Detector type": "DECTRIS PILATUS3 6M",
    "Collection date": "Jul 22, 2016",
    Monochromator: "Monochromatic",
    Protocol: "Single wavelength",
  });
  assert.deepEqual(facts(view, "source"), {
    Source: "Synchrotron",
    Type: "ESRF BEAMLINE ID30B",
    Wavelength: "0.969 Å",
    "Synchrotron site": "ESRF",
    Beamline: "ID30B",
  });
});

test("should read the refinement statistics of the model's own file", () => {
  // when / then: the mean isotropic B is unrecorded here and drops its row
  assert.deepEqual(facts(experimentView(ENTRY, null, CIF), "refinement"), {
    "Resolution range": "47.26 – 1.85 Å",
    "Reflections used": "36,853",
    "R-free reflections": "1,848 (5.0%)",
    "Percent reflections observed": "99.4%",
    "R-factor observed": "0.186",
    "R-work": "0.184",
    "R-free": "0.226",
  });
});

test("should put each statistic beside its highest-resolution shell", () => {
  // when
  const collection = block(experimentView(ENTRY, null, CIF), "collection");

  // then: R-merge is in neither column here, so it has no row
  assert.deepEqual(collection.columns, ["Overall", "Highest-resolution shell"]);
  assert.deepEqual(
    collection.rows.map((row) => [row.label, row.overall, row.shell]),
    [
      ["Resolution range", "47.26 – 1.85 Å", "1.92 – 1.85 Å"],
      ["Reflections", "36,867", "3,528"],
      ["Completeness", "99.5%", "96.4%"],
      ["Redundancy", "2.0", "2.0"],
      ["I / σ(I)", "13.48", "1.08"],
      ["R-pim", "0.031", "0.642"],
      ["CC½", null, "0.574"],
    ],
  );
});

test("should take the finest shell when the file records several", () => {
  // given
  const shells = `data_S
loop_
_reflns_shell.d_res_low
_reflns_shell.d_res_high
_reflns_shell.percent_possible_all
_reflns_shell.pdbx_ordinal
47.26 3.90 99.9 1
2.10  1.95 98.4 2
1.95  1.85 96.4 3
#
`;

  // when
  const collection = block(experimentView(ENTRY, null, shells), "collection");

  // then
  const completeness = collection.rows.find((row) => row.label === "Completeness");
  assert.equal(completeness.shell, "96.4%");
});

test("should list the software by what each program was for", () => {
  // when / then: PHENIX writes its build after a colon, and "." is no version
  assert.deepEqual(rows(experimentView(ENTRY, null, CIF), "software"), [
    ["PHENIX", "Refinement", "1.11.1_2575"],
    ["XDS", "Data reduction", null],
    ["XDS", "Data scaling", null],
    ["PHASER", "Phasing", null],
  ]);
});

test("should prefer the entry's own record over the file", () => {
  // given: the record says 1.42 Å in space group C 2, the file says otherwise
  const entry = { ...ENTRY, resolution: 1.42, space_group: "C 2", method: "CryoEM" };

  // when
  const view = experimentView(entry, null, CIF);

  // then
  assert.equal(facts(view, "snapshot").Resolution, "1.42 Å");
  assert.equal(facts(view, "snapshot").Method, "CryoEM");
  assert.equal(facts(view, "cell")["Space group"], "C 2");
});

test("should take pH and temperatures from the entry's crystals when it has them", () => {
  // given
  const crystallography = crystallographyView({
    crystals: [
      {
        id: "1",
        growth: { ph: 5.5, temperature_kelvin: 291 },
        diffractions: [{ id: "1", temperature_kelvin: 80 }],
      },
    ],
  });

  // when
  const view = experimentView(ENTRY, crystallography, CIF);

  // then
  assert.equal(facts(view, "crystallization").pH, "5.5");
  assert.equal(facts(view, "crystallization").Temperature, "291 K");
  assert.equal(facts(view, "diffraction")["Collection temperature"], "80 K");
  // one dataset says everything it has to say in the facts above
  assert.equal(block(view, "datasets"), undefined);
});

test("should keep the dataset table when the entry records more than one", () => {
  // given
  const crystallography = crystallographyView({
    crystals: [
      {
        id: "1",
        growth: { ph: 5.5 },
        diffractions: [
          { id: "1", temperature_kelvin: 80 },
          { id: "2", temperature_kelvin: 100 },
        ],
      },
    ],
  });

  // when / then
  const view = experimentView(ENTRY, crystallography, CIF);
  assert.equal(block(view, "datasets").view.datasets.length, 2);
});

test("should have nothing to show for an entry with no record and no file", () => {
  // when / then
  assert.equal(experimentView({ ...ENTRY }, null, null), null);
});

test("should still show what the record holds when there is no file", () => {
  // given
  const entry = { ...ENTRY, method: "X-ray crystallography", resolution: 2.1 };

  // when
  const view = experimentView(entry, null, null);

  // then
  assert.deepEqual(facts(view, "snapshot"), {
    Method: "X-ray crystallography",
    Resolution: "2.10 Å",
  });
  assert.deepEqual(
    view.blocks.map((item) => item.key),
    ["snapshot"],
  );
});
