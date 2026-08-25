# Dynamic PDB Specification

## 1. Why it exists

### Problem

Structural models change and improve over time. Without one registry, it is hard
to find the data recorded with each Model and compare their quality scores.

### Users

- **Visitors** find, inspect, and download public data.
- **Depositors** submit datasets and models.
- **Reviewers** approve or reject submissions.

### Value

Dynamic PDB keeps raw data, models, processing runs, and quality metrics in one
traceable history. Researchers can follow how a structure evolves and compare
each model by its recorded scores.

### Scope

Dynamic PDB is a registry, not a modeling service. It stores or references
results; it does not run model building or refinement. Files can be downloaded
one at a time. Bulk download is not implemented.

## 2. How it works

### Core concepts

Dynamic PDB records:

- **Data:** a stored file or external file reference.
- **Run:** a program execution with its software, version, inputs, and outputs.
- **Model:** a structural model produced outside Dynamic PDB.
- **Metrics:** numeric quality scores attached to a Model.

An Entry groups related Data and Models. A Revision is a reviewable snapshot of
an Entry or Model. Data and results use four levels:

| Level | Meaning |
| --- | --- |
| L0 | Raw source data, including FASTA. |
| L1 | Processed experimental data, such as MTZ or maps. |
| L2 | Structural models, such as PDB or mmCIF. |
| L3 | Evaluations and validation results. |

### Browse and search

Browsing is public. Search covers approved Entries, Models, Artifacts, and their
metadata. Unapproved revisions stay private.

### Entries and models

An Entry page shows metadata, source files, Models, and similar proteins. A
Model page shows its files, data levels, metrics, download link, and processing
pipeline.

### Structure viewer and quality metrics

Mol* displays PDB and mmCIF structures.

The UI displays R-work, R-free, RSCC, and CC when supplied. Values outside fixed
quality thresholds are highlighted:

| Metric | Warning | Bad |
| --- | --- | --- |
| R-work, R-free | `>= 0.25` | `>= 0.30` |
| RSCC, CC | `< 0.90` | `< 0.80` |

### Web submission

A signed-in user can create an Entry or add a Model. Files may be local, an HTTP
URL, or from public Ext. Known file fields prefill metadata, runs, and metrics.
Each Model needs exactly one PDB or mmCIF file. The form saves unfinished Entries
in the browser. New Entries and Models enter review.

### Batch upload with the CLI

The CLI follows one visible plan:

```text
login -> scan folder -> review YAML manifest -> upload
```

The CLI scans folders and ZIP archives, groups files by PDB ID, and can import
from RCSB. YAML rules extract JSON, CSV, TSV, PDB, or mmCIF values. Uploads
support concurrency, filters, and restart from a local state log.

### Files and provenance

PostgreSQL stores file metadata. Bytes live in S3, at an HTTP URL, or in Ext.
Clients upload up to 1 GiB directly through presigned S3 URLs.

A Run describes one program execution: the software, its version, input files,
and output files. The Model page shows these Runs as a pipeline graph.

### Revisions and review

Every submitted Entry and Model is a separate revision:

```text
in review -> active -> archived (when replaced with newer version)
in review -> rejected
```

Public reads return only the active revision. Reviewers use a diff and preview
to decide each Entry and Model separately. A new Entry must be approved before
its Models. Depositors see their own under-review and rejected work.

### Similar proteins

An Entry page suggests Entries with similar protein sequences. This helps
researchers find related structures and compare their Models. Each match shows
identity, coverage, score, and alignment. A daily MMseqs job calculates the
matches from FASTA files in active Entries.

### Authentication and permissions

Public reads need no account. Writes require GitHub login and membership in an
allowed organization: `Astera-org` or `diff-use`.

The backend exchanges GitHub authorization for its own JWT. The website keeps it
in an HTTP-only cookie; the CLI keeps it in local config. Separate permissions
control approval and rejection.

## 3. Architecture and deployment

### Runtime architecture

Dynamic PDB has two clients and one backend:

- The **Next.js website** provides the interactive UI.
- The **Go CLI** provides batch submission.
- The **backend** handles catalog data, authentication, submission, and review.

The backend stores catalog data, revisions, provenance, and similarities in
PostgreSQL. Scientific files live in S3 and are served through CloudFront.
The browser and CLI upload files directly to S3 using URLs issued by the backend.

### Production

Helm deploys these workloads to the `dynamicpdb` Kubernetes namespace:

- two website pods;
- two backend pods;
- two S3 proxy pods for CLI installers and releases;
- a Flyway Job for database migrations;
- a daily MMseqs CronJob with a persistent 16 GiB cache.

Traefik exposes everything on `dynamicpdb.com`:

| Path | Destination |
| --- | --- |
| `/` | Website |
| `/api` | Backend |
| `/install.sh`, `/releases`, `/config` | S3 proxy |

PostgreSQL and S3 run outside this chart. Kubernetes Secrets provide their
credentials and the application secrets.

### Delivery

GitHub Actions tests the affected code. A successful `main` build pushes images
tagged with the commit SHA to Harbor. ArgoCD deploys that SHA with Helm.

A `v*` tag publishes CLI archives for Linux and macOS to the release S3 bucket.
