Public records and their available files can be accessed without an account. Use the website to explore individual entries and the API to retrieve records programmatically.

| Use case | Access method |
| --- | --- |
| Inspect a protein and download associated files | Entry and model pages |
| Retrieve metadata and file references for scripts or agents | API |
| Download an entire collection in one operation | Bulk download is not currently implemented |

## Download individual files

Open an entry or model and select an available file's download link. Files may be stored by The Dynamic PDB or linked from an external source. Available content varies by entry and may include experimental data, coordinates, and supporting files.

## Use the API

The API provides public records without authentication. Responses use JSON format, with records inside a `data` field. Scripts and LLM agents should use these structured records to retrieve metadata and file references. The [API reference](/docs/api) describes every endpoint and request format.

For reproducible analysis, retain the entry and model identifiers, file checksums where available, and the date of retrieval. Public records may change when a newer revision is approved.

### 1. Find an entry by PDB ID

```bash
curl -fsS \
  -H 'Accept: application/vnd.api+json' \
  'https://dynamicpdb.com/api/v1/entries?pdb_id=7C24'
```

Use the returned `data[].id` for subsequent requests. The entry ID in The Dynamic PDB is distinct from its external PDB reference.

### 2. List the entry's models

```bash
curl -fsS \
  -H 'Accept: application/vnd.api+json' \
  'https://dynamicpdb.com/api/v1/entries/dpdb_ube2dzua/models?limit=100&offset=0'
```

Each model includes its identifier, metadata, and reported metrics.

### 3. Find a model's coordinate file

```bash
curl -fsS \
  -H 'Accept: application/vnd.api+json' \
  'https://dynamicpdb.com/api/v1/entries/dpdb_ube2dzua/models/dpdb_ube2dzua_m_002/artifacts?levels=L2&limit=100&offset=0'
```

The API calls file records **artifacts**. Each record can include its name, format, checksum, size, and file location in `attributes.uri`.

L2 results can include both coordinates and supporting files, such as refinement logs. Select the coordinate record using its type and format, or match its ID to the model's `primary_artifact_id`.

### 4. Download the selected file

Copy the selected record's HTTP or HTTPS `attributes.uri` into this command:

```bash
curl -fL 'REPLACE_WITH_FILE_URI' -o model.pdb
```

Use an output extension that matches the file format.

## Retrieve larger collections

Bulk download is not currently implemented. Scripts can enumerate records through the API and download available files individually.

List endpoints support `limit` and `offset`, with a maximum of 100 records per request. To retrieve additional pages, use `limit=100` and increase `offset` to `100`, `200`, and so on until an empty `data` array is returned. Retrieve entry-level and model-level files separately.

The [OpenAPI specification](/api/openapi.yaml) is a machine-readable description of the API, suitable for scripts and LLM agents.

Before reusing data, check the applicable license and attribution requirements, including those of externally referenced sources. Data originating from The Dynamic PDB and Prism are freely available for community use. See [Reuse, acknowledgment, and feedback](/docs/reuse).
