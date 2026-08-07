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

The command prints a GitHub device code, opens the browser when possible, copies
the code to the clipboard when possible, and stores the Dynamic PDB token after
authorization.

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
./cli/bin/dynamic-pdb manifest init /path/to/data \
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

### 3. Review and edit the manifest

The manifest is intentionally editable. Fill in the fields that the CLI
cannot know safely, especially model names, model type, and purpose.

Use `filter.include` when you want to upload only a small subset during testing:

```yaml
version: 1
data_root: /path/to/data

filter:
  include:
    - 1YJO
  skip: []

entries:
  pdb_id: "{{ pdb_id }}"
  name: "{{ pdb_id }}"
  metadata:
    source:
      rcsb: "{{ pdb_id }}"
    fields:
      - title
      - method
      - resolution
      - organism
      - space_group
  preview_image:
    source:
      rcsb: "{{ pdb_id }}"
  artifacts:
    - id: fasta
      source:
        rcsb: "{{ pdb_id }}"
      level: L0
  models:
    - id: model_1
      name: Deposited model
      model_type: Single Conformer
      purpose: Model Building
      artifacts:
        - id: coordinates
          source:
            rcsb: "{{ pdb_id }}"
          level: L2
        - id: structure_factors_1
          source:
            rcsb: "{{ pdb_id }}"
          level: L1
      metrics:
        source:
          rcsb: "{{ pdb_id }}"
        fields:
          - r_free
          - r_work

    - id: model_2
      name: Rerefined model
      model_type: Single Conformer
      purpose: Refinement
      artifacts:
        - id: coordinates
          source:
            file: Rerefined/final_model/{{ pdb_id }}_020.pdb
          level: L2
        - id: log_1
          source:
            file: Rerefined/final_model/{{ pdb_id }}_020.log
          level: L2
        - id: structure_factors_1
          source:
            rcsb: "{{ pdb_id }}"
          level: L1
      metrics:
        source:
          artifact: coordinates
        fields:
          - r_free
          - r_work
```

Useful source forms:

- `file: path/to/{{ pdb_id }}.pdb` reads a local file under `data_root`.
- `file: archive.zip#path/in/archive/{{ pdb_id }}.pdb` reads a file inside a zip.
- `rcsb: "{{ pdb_id }}"` resolves the artifact or metadata from RCSB.
- `artifact: coordinates` lets metrics be parsed from another artifact in the
  same model.

### 4. Upload

```bash
./cli/bin/dynamic-pdb manifest upload /path/to/data/dynamic-pdb.manifest.yaml
```

During upload the CLI shows progress in the terminal. At the end it prints a
summary and writes a JSON upload log next to the manifest:

```text
Uploaded 1 entries, 3 models, 7 artifacts.
Upload log: /path/to/data/dynamic-pdb.manifest.upload-log.json
```

The JSON log is meant to be easy to parse. It contains uploaded entry IDs, model
IDs, artifact IDs, run IDs, metric IDs, skipped PDB IDs, and the per-entry
breakdown.
