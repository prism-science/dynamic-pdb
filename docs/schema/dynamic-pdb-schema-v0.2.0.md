# Dynamic PDB Schema v0.2.0

> Draft dated 2026-10-16. `backend/api/openapi.yaml` is the enforced API contract; this document is the scientific specification it is reviewed against.

> Requirements below describe the intended schema. Implementation gaps come from the previous audit (API commit fc1f6ee, migrations through V20260907120000__polymer_entities.sql), not a fresh production check.

## Purpose and scientific limits

Dynamic PDB connects one experimental context (entry) to multiple structural interpretations (models). A PDB archive model is a peer, with no preferred scientific status.

Acceptance requires documented sources and processing, not proof of correctness or wwPDB endorsement. Validation scores and residue measurements are optional; show their absence.

Every file has a processing level:

| Level | Meaning | Examples |
|---|---|---|
| L0 | Source measurements before processing choices that affect interpretation. | Raw diffraction images, detector datasets. |
| L1 | Processed experimental data. | Reflection files, density maps. Record processing and any model dependence. |
| L2 | Structural interpretations. | Coordinates, alternate conformations, ensembles, trajectories. |
| L3 | Evaluations against experimental data or expected geometry. | Fit statistics, validation reports. |

An entry need not contain every level. Each L2 model declares its source data, even when those data are held elsewhere.

## Database structure and versioning

| Object | Store and link |
|---|---|
| Entry | Stable identity for an experimental context. Has many entry revisions and models. |
| Entry revision | Experimental metadata; links to artifacts and polymer entities. |
| Model | Stable identity for one interpretation. Belongs to exactly one entry; has many model revisions. |
| Model revision | Model metadata; links to artifacts, metrics, runs and interaction sets. Holds residue data as embedded JSON. |
| Artifact | One file, shareable across revisions and runs. |
| Run | One software execution, linked to input/output artifacts and associated model revisions. |

Use join tables for many-to-many links. Derive processing history from run inputs and outputs.

Changing metadata belong on revisions: required title, increasing revision_number, parent_revision_id, published_at, and optional change_summary. The first revision has no parent; unpublished revisions have no publication date. Allow at most one active revision per entry or model.

| Vocabulary | Allowed values |
|---|---|
| Entry/model state | new, active, deleted — soft deletion, separate from revision status. |
| Revision state | pending, in_review, active, rejected, archived. |

Entry IDs: dpdb_ plus eight lowercase letters or digits. Model IDs: dpdb_xxxxxxxx_m_NNN. Assigned at creation; no DOI. Published revision identifiers are required but unimplemented, so object IDs cannot identify exact versions.

## Experimental metadata

Extract metadata from files and archives where possible. Depositors supply titles, constructs, mutations and missing context. Record each value's source; preserve depositor annotations.

| Entry-revision field | Meaning and rule |
|---|---|
| title | Required text; replaces separate revision name and description. |
| external_refs.pdb | Optional source accession, unique across active entries. pdb is the only supported key. |
| method | Required experimental method. Free text today; OBI/EFO ontology identifiers remain planned. |
| resolution | Optional positive number in ångströms, describing highest measured resolution. Omit when undefined for the method. |
| space_group | Optional crystallographic symmetry in Hermann–Mauguin notation, such as P 21 21 21. |
| crystallography.crystals[] | Per-crystal growth.ph, growth.temperature_kelvin, and diffractions[].temperature_kelvin. Keep growth and collection conditions separate; do not flatten multiple crystals into one value. |

A polymer entity is a distinct sequence or construct; multiple chains may share it. Preserve registry id and source label_entity_id; link entities to entry revisions.

| Polymer-entity field | Meaning and rule |
|---|---|
| description, construct | Molecular description and optional experimental construct description. |
| source_organisms[] | Required array of {scientific_name, ncbi_taxonomy_id}; may be empty. Ontology release pinning remains open. |
| mutations[] | Substitution strings, such as ["C54T", "C97A"], replacing comma-joined text. Reference sequence and numbering convention still need an explicit rule. |
| uniprot_mappings[] | Required accession and source per mapping. Sources: sifts, struct_ref, alignment. Require unp_release for sifts and alignment. |

UniProt positions resolve on residue records through uniprot_position, for example P00720:42. Separate mapping-segment arrays are not required.

Ligand and cofactor lists belong on the entry revision, describing sample composition rather than binding partners. Move these lists off models. Store CCD chemical-component identifiers. Exclude solvent, buffer and cryoprotectants; retain catalytic and structural metals. Empty arrays mean known absent; omitted arrays mean unknown. An empty cofactor list alone does not establish an apo sample.

Specific ligand instances and their residue contacts belong to model revisions, as defined below. Different models of the same experiment may show different interactions.

## Model and residue metadata

| Model-revision field | Meaning and rule |
|---|---|
| title | Required depositor text. |
| model_type | Single Conformer, Multiconformer, Ensemble, Deposited. The first three describe representation; Deposited denotes an unchanged archive import. This inherited list mixes representation and origin. |
| purpose | Model Building, Refinement, Reference. Archive imports use Reference, meaning comparison context rather than preferred truth. |
| model_source | pdb or ext: origin of the coordinate file. |
| authors, affiliation | Attribution from the archive or depositor. |
| atom_count | Non-hydrogen atoms. |
| modeled_residues | Polymer residues with coordinates, across chains. |
| unique_protein_chains | Distinct protein chains in the coordinates. |
| altloc_fraction | Fraction of modeled residues with alternate positions; count a second position only at occupancy ≥0.1. Count residues, not atoms. |
| unmodeled_fraction | One minus modeled residues divided by full deposited sequence length, matching each chain to its entity sequence. |

Counts and fractions are optional registry calculations. Fractions range from 0 to 1. Prefer mmCIF coordinates; use legacy PDB files only within that format's limits.

Residue data are optional and embedded on the model revision:
- Identify polymer residues by label_asym_id (chain), label_seq_id (sequence position), and label_comp_id (CCD residue identity).
- Preserve author chain/residue numbering (auth_asym_id, auth_seq_id) and insertion codes (pdbx_pdb_ins_code) where available.
- Use label_alt_id for conformer-specific values. A residue key alone cannot distinguish alternate conformers.
- Supported values: rscc, b_iso (mean atomic displacement parameter), occupancy (of the identified conformer), conformer_count, rmsf (ensemble positional spread), and uniprot_position.
- Do not infer local measurements from global averages. Ensemble-member identity and aggregation rules remain to be specified.

## Residue–ligand interactions

Support searches such as "histidines coordinating zinc" and "lysines contacting ATP." Store searchable interaction sets linked to model revisions; each set contains atom-pair interaction records. These are optional and do not determine model acceptance.

Import explicit connections from mmCIF _struct_conn. Compute additional contacts from _atom_site coordinates. Keep imported annotations distinguishable from computed contacts: _struct_conn is not a complete inventory of ligand contacts.

| Interaction-set field | Required content |
|---|---|
| id, model_revision_id, artifact_id | Identify the set, exact model revision and coordinate artifact. |
| source, run_id | Source is mmcif_struct_conn or computed. Link to the importing or computing run, with software/version. |
| status, scope | Status: pending, running, complete, failed. Scope records included chains, ligands, members, conformers and assembly/symmetry context. |
| parameters | Contact criteria, including distance/angle thresholds, atom selection, occupancy handling and alternate-conformer compatibility rules. For imports, identify the source category. |

| Interaction-record field | Required content |
|---|---|
| id, interaction_set_id | Identify the record and its parent set. |
| residue_atom, ligand_atom | Each identifies a specific atom using the source atom_site.id; retain chain, component, residue numbering, atom name, model number and alternate-location identifier for searching and display. |
| ligand_instance_id | Identify this particular ligand molecule within the coordinate artifact, separately from its CCD code. Do not require a polymer label_seq_id for ligands. |
| interaction_type | covalent, metal_coordination, hydrogen_bond, proximity, or other. Preserve the original connection type and _struct_conn.id for imported records. |
| distance_angstrom, geometry | Atom-pair distance when available, plus any angles used in classification. |
| partner_transforms | Identity, crystallographic symmetry or assembly transformation for each partner. Distinguish crystal contacts from contacts within the selected biological assembly. |

Partner atoms must resolve within the set's coordinate artifact. Keep ensemble members and alternate conformers distinct; never create contacts between incompatible states. Any across-member summary must state its denominator and weighting.

A proximity rule could select non-hydrogen atom pairs within 4 Å, but this is an example, not a universal binding criterion. Hydrogen bonds and metal coordination need their own documented criteria. Geometric proximity alone does not establish binding affinity or biological function.

Allow filtering by residue type, ligand CCD code, interaction type and evidence source; return the exact revision and ligand instance. Deduplicate atom-pair hits when returning residues. No interaction set means not assessed; an empty completed set means no matches under its recorded scope and criteria. Neither establishes absence of biological binding.

## Files and processing history

Preserve the original mmCIF file unchanged. Extract selected fields for search and comparison; database metadata do not replace the file or reproduce the full mmCIF dictionary. Retain all file content, including fields the registry does not extract. Store any converted or modified file as a separate artifact.

Each artifact requires name, level, type, and a registry-computed sha256 of its exact bytes: 64 lowercase hexadecimal characters. Artifact types are model, structure_factors, fasta, other. Type is role; level is processing; format is encoding.

Optional fields are lowercase format, nonnegative size_bytes, and format-specific metadata. Format is an open vocabulary: examples include mmcif, mtz, mrc, fasta, and structure_factors_cif. Record unfamiliar formats rather than rejecting them. FASTA metadata contains records[] with header and sequence. Metadata keys beginning with __ are reserved.

Require a stored/external URI or accession resolving to the exact file. Whether the registry stores its own copy or relies on an external archive remains an open preservation policy; either approach must retain access to the unchanged original.

Every computed model needs at least one run recording software_name, software_version, and explicit input/output artifact links (direction: input or output). Separate model building, refinement and later processing into their actual executions. Record the verbatim command and structured JSON parameters where available. An unchanged archive import may use its source accession as provenance.

## Validation and trustworthy comparison

Metrics attach to model revisions. Record each metric's key, numeric value and producing software; also record software version where possible. Missing means unavailable, never zero. Show affected models as unranked for that metric.

| Metric keys | Meaning | Interpretation |
|---|---|---|
| r_work, r_free | Agreement with refinement data and held-out reflections. | Lower is better under comparable evaluation conditions. |
| cc, rscc | Overall and local correlation with data or density. | Higher is better under comparable evaluation conditions. |
| clashscore | Severe atomic overlaps per 1,000 atoms. | Lower is better. |
| ramachandran_outliers, side_chain_outliers | Percentages of backbone and side-chain geometry outliers. | Lower is better. |
| molprobity_score | Composite geometry score. | Lower is better. |
| wilson_b, rmsf | Diffraction fall-off and ensemble positional spread. | Context-dependent, not quality rankings. |

Do not rank models by alternate-conformer fraction, completeness or conformer count alone. More conformers do not establish better fit or greater physical motion.

For scientific review, expose exact files, residue numbering, processing history and validation limitations. R-free comparisons require matching held-out reflections and compatible settings. Preserve these details in parameters or reports; structured fields remain to be specified.
