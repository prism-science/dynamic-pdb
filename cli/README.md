# Dynamic PDB CLI

`dynamic-pdb` is a console client for uploading model datasets into Dynamic PDB.
It is meant for batch uploads: point it at a folder with coordinate files, logs,
maps or structure factors, let it create a manifest, review the manifest, then
upload everything in one run.

The CLI does not try to make hidden decisions during upload. The generated
manifest is the plan: it shows which PDB entries will be created,
which models will be attached to each entry, which files belong to each model,
and which metadata or remote artifacts will be pulled from RCSB.

## Build

```bash
cd cli
make build
./bin/dynamic-pdb --help
```

From the repository root, the binary is usually available at:

```bash
./cli/bin/dynamic-pdb
```

## Install and update

Release builds are installed from `dynamicpdb.com`:

```bash
curl -fsSL https://dynamicpdb.com/install.sh | bash
```

Update an installed release in place:

```bash
dynamic-pdb update
```

Pin a specific version with `dynamic-pdb update --version vX.Y.Z`.

## Configure

Login uses GitHub device authorization. The CLI stores its config under
`$XDG_DATA_HOME/dynamic-pdb/config.toml`, or under
`~/.local/share/dynamic-pdb/config.toml` when `XDG_DATA_HOME` is not set.

For local development, create or edit the config before login:

```toml
[server]
url = "http://localhost:8080"

[github]
client_id = "your-github-oauth-client-id"
```

Then authenticate:

```bash
./cli/bin/dynamic-pdb login
```

The command prints a GitHub device code. Open the shown URL, enter the
code, and the CLI stores the Dynamic PDB token after authorization.

To clear saved auth:

```bash
./cli/bin/dynamic-pdb logout
```

## User Flow

### 1. Put the dataset in one folder

The input is a data folder. It may contain nested folders and zip archives.
The manifest generator looks for model-like files by filename pattern and file
extension. The current upload flow uses:

- coordinate files: `.pdb`, `.ent`, `.cif`, `.mmcif`
- logs: `.log`
- structure factors or map-like inputs: `.mtz`, structure-factor `.cif`

Every entry is matched to a PDB ID. If the CLI cannot match a file to a PDB ID,
that file is not part of the generated upload plan.

### 2. Generate a manifest

```bash
./cli/bin/dynamic-pdb upload manifest init /path/to/data \
  --out /path/to/data/dynamic-pdb.manifest.yaml
```

The command writes a manifest and prints a short summary:

```text
Wrote /path/to/data/dynamic-pdb.manifest.yaml
Manifest written.
PDB IDs: 2
Local files: 9
```

If `--out` is omitted, the manifest is written as `dynamic-pdb.manifest.yaml`
inside the data folder.

Pass `--include-rcsb-model` to include the deposited RCSB structure as the first
model in the generated manifest.

### 3. Review and edit the manifest

The manifest is intentionally editable. Fill in the fields that the CLI
cannot know safely, especially model titles, model type, and purpose.

Use `filter.include` when you want to upload only a small subset during testing:

```yaml
version: 1
data_root: /path/to/data

filter:
  include:
    - 1YJO
  skip: []

entries:
  - pdb_id: "{{ pdb_id }}"
    title: "{{ pdb_id }}"
    metadata:
      title:
        source:
          rcsb:
            pdb_id: "{{ pdb_id }}"
            resource: entry
        extract:
          json:
            field: struct.title
      method:
        source:
          rcsb:
            pdb_id: "{{ pdb_id }}"
            resource: entry
        extract:
          json:
            field: exptl[0].method
      resolution:
        source:
          rcsb:
            pdb_id: "{{ pdb_id }}"
            resource: entry
        extract:
          json:
            field: rcsb_entry_info.resolution_combined[0]
      organism:
        source:
          rcsb:
            pdb_id: "{{ pdb_id }}"
            resource: polymer_entity
        extract:
          json:
            field: rcsb_entity_source_organism.ncbi_scientific_name
      space_group:
        source:
          rcsb:
            pdb_id: "{{ pdb_id }}"
            resource: entry
        extract:
          json:
            field: symmetry.space_group_name_H_M
    preview_image:
      source:
        rcsb:
          pdb_id: "{{ pdb_id }}"
          file: "{{ pdb_id }}_assembly-1.jpeg"
    artifacts:
      - id: fasta
        source:
          rcsb:
            pdb_id: "{{ pdb_id }}"
            resource: fasta
        level: L0
    models:
      - id: model_1
        title: Deposited model
        model_type: Single Conformer
        purpose: Model Building
        artifacts:
          - id: coordinates
            source:
              rcsb:
                pdb_id: "{{ pdb_id }}"
                file: "{{ pdb_id }}.cif"
            level: L2
          - id: structure_factors_1
            source:
              rcsb:
                pdb_id: "{{ pdb_id }}"
                file: "{{ pdb_id }}-sf.cif"
            level: L1
        metrics:
          r_free:
            source:
              rcsb:
                pdb_id: "{{ pdb_id }}"
                resource: entry
            extract:
              json:
                field: refine[0].ls_R_factor_R_free
          r_work:
            source:
              rcsb:
                pdb_id: "{{ pdb_id }}"
                resource: entry
            extract:
              json:
                field: refine[0].ls_R_factor_R_work

      - id: model_2
        title: Rerefined model
        model_type: Single Conformer
        purpose: Refinement
        artifacts:
          - id: coordinates
            source:
              files:
                - Rerefined/final_model/{{ pdb_id }}_020.pdb
            level: L2
          - id: log_1
            source:
              files:
                - Rerefined/final_model/{{ pdb_id }}_020.log
            level: L2
          - id: structure_factors_1
            source:
              rcsb:
                pdb_id: "{{ pdb_id }}"
                file: "{{ pdb_id }}-sf.cif"
            level: L1
        metrics:
          r_free:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: REMARK 3 FREE R VALUE
          r_work:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: REMARK 3 R VALUE WORKING SET
```

Useful source forms:

- `files: [path/to/{{ pdb_id }}.pdb]` reads local files under `data_root`.
- `files: [archive.zip#path/in/archive/{{ pdb_id }}.pdb]` reads files inside a zip.
- `rcsb.pdb_id` selects the PDB entry. Use `rcsb.resource` for RCSB API resources such as `entry`, `polymer_entity`, or `fasta`, and `rcsb.file` for downloadable RCSB files such as `{{ pdb_id }}.cif` or `{{ pdb_id }}-sf.cif`.
- `artifact: coordinates` lets metrics be parsed from another artifact in the
  same model.

CSV and TSV metadata tables can be used with `extract.csv` or `extract.tsv`:

```yaml
metadata:
  atom_count:
    source:
      files:
        - Rerefined/final_model_structure_table.tsv
    extract:
      tsv:
        column: Atom Count
        where:
          column: ID
          equals: "{{ pdb_id }}"
```

### 4. Upload

```bash
./cli/bin/dynamic-pdb upload start /path/to/data/dynamic-pdb.manifest.yaml
```

To temporarily filter an upload without editing the manifest, pass `--include`
or `--skip`. These flags override the corresponding manifest `filter.include`
and `filter.skip` lists:

```bash
./cli/bin/dynamic-pdb upload start /path/to/data/dynamic-pdb.manifest.yaml \
  --include 1YJO,5AMF \
  --skip 6ABC
```

For a recognized Sampleworks result folder, `upload start` can take the folder
path directly. The CLI writes `dynamic-pdb.manifest.yaml` inside that folder,
prints the generated path, and then uploads from that manifest.

During upload the CLI shows progress in the terminal. It also writes a JSON
upload state next to the manifest:

```text
Uploaded 1 entries, 3 models, 7 artifacts.
Upload state: /path/to/data/dynamic-pdb.manifest.upload.jsonl
```

The upload state is an append-only JSONL file:

```jsonl
{"event":"entry_uploading","pdb_id":"1YJO","at":"2026-08-10T12:00:00Z"}
{"event":"entry_completed","pdb_id":"1YJO","entry_id":"...","model_ids":["..."],"artifact_ids":["..."],"run_ids":["..."],"metric_ids":["..."],"at":"2026-08-10T12:03:00Z"}
```

Completed entries are skipped on restart. If an entry is still marked as
`entry_uploading` without a later `entry_completed`, the upload will stop until
the failed upload is fixed and that entry is removed from the state file.
