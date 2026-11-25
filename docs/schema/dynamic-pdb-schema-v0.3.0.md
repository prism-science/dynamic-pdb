# Dynamic PDB Schema v0.3.0

> `backend/api/openapi.yaml` is the enforced API contract; this document is the scientific specification it is reviewed against. `docs/cif-extraction.md` holds the detailed file-reading rules.

> Requirements below describe the intended schema. Where a requirement is not implemented yet or a question is still open, the text says so; "Schema backlog" lists the fields agreed for later.

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
| method | Required experimental method. A depositor picks X-ray crystallography or CryoEM; an entry created for a PDB ID may keep the archive's method. Entries the archive sync imports keep the archive's text (for example NEUTRON DIFFRACTION). OBI/EFO ontology identifiers remain planned. |
| resolution | Optional positive number in ångströms, describing highest measured resolution. Omit when undefined for the method. |
| space_group | Optional crystallographic symmetry in Hermann–Mauguin notation, such as P 21 21 21. |
| crystallography.crystals[] | Per-crystal growth.ph, growth.temperature_kelvin, and diffractions[].temperature_kelvin. Keep growth and collection conditions separate; do not flatten multiple crystals into one value. |
| related_datasets[] | Optional datasets held elsewhere that relate to this entry, as mmCIF _pdbx_related_exp_data_set records them: required data_type; a doi, an accession (the repository's own ID, for datasets without a DOI) or both; optional landing_url (an http or https page), repository and details; optional relation and link_evidence (since v0.3.0; see Related dataset relation). References, not artifacts: no copy and no checksum. Set by people only; archive synchronisation never changes them. They do not record which data a model was refined against. Cite a dataset's version DOI, not a concept DOI that moves to the newest version. related_datasets is for data; related entries (_pdbx_database_related, on models) are for structures. |

### Related dataset relation (v0.3.0)

Each related dataset may state how it relates to the experiment the entry describes, and what shows the link. Both are optional; absent means not stated. Both are off on dev and production until their switch-on (entries.related_dataset_relation_enabled); a server with them off refuses either key with 422.

| relation | Meaning |
|---|---|
| source_data | The deposited coordinates were refined against data reduced from this dataset. |
| similar_sample | Collected from comparable crystals or samples, not the deposited one. If the collection temperature differs from the entry's, details says so. |
| other | Any other relationship; details must say what it is. |

| link_evidence | Meaning |
|---|---|
| pdb_record | The PDB entry cites the dataset. |
| repository_record | The repository's metadata names the PDB ID. |
| repository_text | The repository's page text names the PDB ID. |
| publication | A paper's data availability statement names both. |
| depositor_statement | The depositor states it. |

- link_evidence is required when relation is set; link_evidence alone is allowed.
- pdb_record does not imply source_data: a PDB entry can cite a control sample or one of several collections. Reviewers confirm the relation row by row.
- Values are closed on write (422 at the field's pointer). Readers must accept values added later and show an unknown one as text.
- When the entry has no PDB ID, compare against the experiment the entry describes.
- A revision's timestamps record when a person confirmed the list, not that a DOI still resolves.

mmCIF has no item for either field: _pdbx_related_exp_data_set (PDBx v5.419) holds ordinal, data_reference, metadata_reference, data_set_type, details and db_source, and IHM (v1.29) records how data were used (_ihm_dataset_group.application) and derivation between datasets (_ihm_related_datasets), not same or similar sample. Both are Dynamic PDB extensions in their own category, so no wwPDB-owned category gains items:

| Field | mmCIF (extension) | DataCite relationType | PROV |
|---|---|---|---|
| relation source_data | _dpdb_related_exp_data_set.relation | IsDerivedFrom | prov:wasDerivedFrom |
| relation similar_sample | _dpdb_related_exp_data_set.relation | References | none |
| relation other | _dpdb_related_exp_data_set.relation | no mapping | none |
| link_evidence | _dpdb_related_exp_data_set.link_evidence | none | none |

_dpdb_related_exp_data_set.ordinal points to _pdbx_related_exp_data_set.ordinal. The existing mapping (doi to data_reference, data_type to data_set_type, repository to db_source, details to details) does not change.

A polymer entity is a distinct sequence or construct; multiple chains may share it. Preserve registry id and source label_entity_id; link entities to entry revisions.

| Polymer-entity field | Meaning and rule |
|---|---|
| description, construct | Molecular description and optional experimental construct description. |
| source_organisms[] | Required array of {scientific_name, ncbi_taxonomy_id}; may be empty. Ontology release pinning remains open. |
| mutations[] | Substitution strings, such as ["C54T", "C97A"], replacing comma-joined text. Reference sequence and numbering convention still need an explicit rule. |
| uniprot_mappings[] | Required accession and source per mapping. Sources: sifts, struct_ref, alignment. Require unp_release for sifts and alignment. |

UniProt positions resolve on residue records through uniprot_position, for example P00720:42. Separate mapping-segment arrays are not required.

Ligand and cofactor lists belong on the entry revision, describing sample composition rather than binding partners. Store CCD chemical-component identifiers. Empty arrays mean known absent; omitted arrays mean unknown. An empty cofactor list alone does not establish an apo sample.

When the lists are derived from a coordinate file:
- Exclude solvents, buffers and cryoprotectants; retain catalytic and structural metals.
- Keep ambiguous compounds (sulfate, phosphate, citrate, fluoride, chloride, bromide, iodide) as ligands flagged for review, since they often come from the buffer.
- Follow the depositor's _pdbx_entity_instance_feature flag: SUBJECT OF INVESTIGATION keeps any compound as a ligand; NO FUNCTIONAL ROLE leaves it out, ambiguous compounds included.
- Keep glycans in the ligand list. Record each branched entity's _pdbx_entity_branch_descriptor rows (WURCS descriptor, program and version): a list of sugar identifiers loses the branching.
- Leave integrative-model probes (_ihm_ligand_probe) out of both lists. An integrative entry's lists are the union across its models, since each model can have its own assembly.
- Record why each compound was placed or left out: the rule and the reference-list version. The record includes compounds left out of both lists; water and polymer residues are never in it.

Specific ligand instances belong to model revisions: modeled compounds and residue contacts, as defined below. Different models of the same experiment may contain different compounds and show different interactions.

## Model and residue metadata

| Model-revision field | Meaning and rule |
|---|---|
| title | Required depositor text. |
| model_type | Single Conformer, Multiconformer, Ensemble, Deposited. The first three describe representation; Deposited denotes an unchanged archive import. This inherited list mixes representation and origin. An Ensemble holds several whole-model states, as MODEL frames or as altloc letters that each trace one conformer along the whole chain. |
| origin | Optional; archive is the only value. Names an unchanged archive deposition whose model_type keeps its representation, such as a re-refinement deposited as an Ensemble. Requires external_refs.pdb and a model file at RCSB's coordinate URI for that ID. Exempts the model from the computed-model rules, as Deposited does, and lets the archive sync keep it current. |
| external_refs.pdb | On a model, the PDB ID of that deposition. One PDB ID belongs to one entry: it cannot be another entry's reference or on a model of another entry. A re-refinement of an entry's data is a model of that entry, not a new entry. |
| related_entries[] | Optional rows of mmCIF _pdbx_database_related under the dictionary's item names: required db_name, db_id and content_type, each db_name and content_type from the dictionary's list, and optional details. For a re-refinement, content_type is re-refinement and db_id names the original deposition. The sync writes them only on models with origin archive. |
| purpose | Model Building, Refinement, Reference. Archive imports use Reference, meaning comparison context rather than preferred truth. |
| model_source | pdb or ext: origin of the coordinate file. |
| authors, affiliation | Attribution from the archive or depositor. |
| atom_count | Non-hydrogen atoms. |
| modeled_residues | Polymer residues with coordinates, across chains. |
| unique_protein_chains | Polymer chain copies; archive models: chains in the deposition; files: chains with ATOM coordinates in the first model. (Researchers' amendment, 17 Nov; the protein chain filter does not use this field.) |
| altloc_fraction | Fraction of the first model's polymer residues whose conformer_count is 2 or more. One rule with conformer_count (below): the same residues, residue key, hydrogen test and occupancy floor. Count residues, not atoms. |
| member_count | Optional, at least 2, Ensemble only. MODEL frames when the file has several; otherwise the altloc letters on every polymer residue. Omitted when the coordinates could not be read to the end, so large files may have none. Never a quality measure. |
| unmodeled_fraction | One minus modeled residues divided by full deposited sequence length, matching each chain to its entity sequence. |

Counts and fractions are optional registry calculations. Fractions range from 0 to 1. Prefer mmCIF coordinates; use legacy PDB files only within that format's limits.

Residue data are optional and embedded on the model revision:
- Identify polymer residues by label_asym_id (chain), label_seq_id (sequence position), and label_comp_id (CCD residue identity). Two residue types at one position (microheterogeneity) are two residues.
- In files without label numbering (PDB format, and mmCIF rows without label_seq_id), map author chain, residue number, insertion code and residue name to label numbering through the entry's deposited mmCIF (_pdbx_poly_seq_scheme). Use the author key only when that fails. Record which key was used: label (from the file), mapped, or author.
- Preserve author chain/residue numbering (auth_asym_id, auth_seq_id) and insertion codes (pdbx_pdb_ins_code) with every residue record.
- Use label_alt_id for conformer-specific values. A residue key alone cannot distinguish alternate conformers.
- Supported values: rscc, b_iso (mean atomic displacement parameter), occupancy (of the identified conformer), conformer_count, rmsf (ensemble positional spread), and uniprot_position.
- conformer_count: the residue's alternate positions (altloc letters) whose highest heavy-atom occupancy in the residue is 0.1 or more; minimum 1. Letters found only on hydrogens never count. Null for ensembles (model_type Ensemble, member_count set, or more than one model frame). altloc_fraction uses the same rule.
- Differences from hetstar's conformer track have three known causes: the occupancy floor, microheterogeneity (hetstar merges residue types at one position), and numbering (hetstar matches residues on author numbering).
- Do not infer local measurements from global averages. Ensemble-member identity and aggregation rules remain to be specified.

Modeled compounds record what the coordinate file contains, as mmCIF keeps _pdbx_nonpoly_scheme complete and records roles elsewhere:
- List every compound the file models outside its polymer chains, in any of its models, buffers included. Exclude only water (HOH, DOD, and the WAT and H2O some programs write). Apply none of the entry-list rules.
- Store one record per copy: CCD identifier, chain (label_asym_id, auth_asym_id), residue number (auth_seq_id, pdbx_pdb_ins_code), entity, and where it was read: _pdbx_nonpoly_scheme, _pdbx_branch_scheme (glycans of two or more sugars), _atom_site with _entity for a residue those categories leave out or a file without them, or the HETATM records of a PDB-format file.
- The registry reads the record from its stored copy of the coordinate file; nobody edits it. A model whose file the registry does not hold keeps the depositor's typed ligand and cofactor lists.
- Roles stay on the entry lists. A display may tag each compound Ligand or Cofactor when the entry's lists name it, otherwise Buffer when the reference lists do; a compound on none of them has no tag.
- Mark a copy "needs review" when the file cannot tell whether it is a compound at all: in a PDB-format chain without SEQRES or an mmCIF file without _entity, a peptide or nucleotide residue, or another residue inside a chain's polymer range. Other compounds in such files are recorded as usual.

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

Require a stored/external URI or accession resolving to the exact file. The registry reads each external file to compute its sha256 and keeps its own copy of files up to 1 GiB. A larger file is checked but only linked, and the record states why no copy was kept. Datasets linked by DOI or accession on the entry revision (related_datasets) are not artifacts and are neither checked nor copied.

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

## Schema backlog

Agreed for later; not part of the schema yet:
- Glycans: a field of their own. Until then glycans stay in the ligand list with their branch descriptors.
- Integrative-model probes: a field of their own. Until then probes are left out of both compound lists and kept in the placement record.
- Linked datasets, model level: when a model is refined against a related dataset, record the chain. Store the repository's checksum manifest (for example SBGrid's files.sha) as an L0 artifact of type other, with a typed related_dataset metadata key naming the entry's related dataset (DOI or accession), since artifact metadata takes only listed keys. Record the manifest's checksum algorithm, inferred from the digest length where the repository does not name it (SBGrid's files.sha holds 40-character SHA-1 digests). A processing run takes the L0 artifact and outputs the L1 reflections; a refinement run takes the L1 file and outputs the L2 model, associated with the model revision. The registry verifies the manifest's own bytes, not the dataset's files: the manifest pins the files, which someone checks against it. If a later entry revision drops the dataset, the model is flagged for review rather than the edit refused.
- mmCIF export of related dataset relations: declare the _dpdb_related_exp_data_set extension through _pdbx_audit_conform_extension (extension_dict_name, extension_dict_version, extension_dict_location) when an mmCIF export exists.

## Versioning

This document and `backend/api/openapi.yaml` carry separate versions. `info.version` in openapi.yaml is the API's; its `info.x-dynamic-pdb-schema-version` is this document's, and a test keeps it equal to this title and the newest change log entry. Every API response carries both, in the `Dynamic-PDB-API-Version` and `Dynamic-PDB-Schema-Version` headers, and every download manifest names both in its header.

| Change | Version step |
|---|---|
| Wording only, no rule changes | PATCH |
| A new optional field or a new vocabulary value | MINOR |
| A new required field, a removed field or value, or a changed meaning | MAJOR |

- Each release is a git tag: `schema-vX.Y.Z` for this document, `api-vX.Y.Z` for the API. Tag names never start with `v`, which triggers the CLI release workflow.
- The file name carries the version. A release renames the file with `git mv`, so its history follows it, and the old path keeps a short note pointing to the old version's tag and to the new file.
- Something deprecated is marked `deprecated: true` in openapi.yaml, listed in the change log, and kept for at least two sprints and 90 days.
- Readers must accept vocabulary values added in a later MINOR version and show an unknown one as text; requests are checked against the closed lists.

## Change log

- **v0.3.0 (2026-11-24):** related datasets may state a relation (source_data, similar_sample, other) and link_evidence (pdb_record, repository_record, repository_text, publication, depositor_statement), both optional; see Related dataset relation. Versioning rules and version headers added. Both fields are off on dev and production until their switch-on.
- **v0.2.0 (2026-10-22):** schema validation core. Changed in place while labelled v0.2.0, without a version step, between 2026-10-22 and 2026-11-24: related datasets, then their accession and landing page (#179); PDB depositions linked as models of their original entry; form API lists (#175); residue index rules (#199). These are part of v0.2.0 as tagged at `schema-v0.2.0`.
