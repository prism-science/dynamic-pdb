# Dynamic PDB - Structural Biology Data Registry

Dynamic PDB is a scientific data registry for structural biology. It connects
source measurements, processed datasets, structural models, provenance links,
and model-quality metrics into one traceable workflow.

This repository is a small monorepo with a Go API, a Next.js website, and
deployment assets:

| Path | What it is |
| ---- | ---------- |
| [`backend/`](backend/) | Go HTTP API for auth, entries, models, entities, provenance relations, search, and file-upload grants |
| [`website/`](website/) | Next.js app for browsing proteins, creating entries, uploading files, linking Ext experiments, and viewing models |
| [`deploy/`](deploy/) | Generalized Helm chart and ArgoCD ApplicationSet for production and dev |
| [`backend/migrations/`](backend/migrations/) | Flyway migrations plus helper CLI for local and deployed database changes |
| [`backend/tools/`](backend/tools/) | Dedicated Go tool module for generators and other project tooling |

The project model and scientific vocabulary are described in [SPEC.md](SPEC.md).

## Architecture

Dynamic PDB is split into a public web surface, a backend API, durable database
state, and object storage for scientific files:

```text
browser
   |
   v
Next.js website ---- GitHub OAuth browser flow
   |                         |
   | same-origin /files      v
   | upload control proxy  backend API ---- GitHub API/org checks
   |                         |
   |                         +-- PostgreSQL
   |                         +-- S3-compatible object storage
   |                         `-- JWT auth
   |
   `-- direct multipart PUTs to presigned object-storage URLs

optional: new-entry form <---- public Ext API/files
```

The backend is the system of record. It exchanges GitHub OAuth tokens or
authorization codes for backend JWTs, gates authenticated writes by allowed
GitHub organizations, stores entries/models/entities in PostgreSQL, builds a
search index, and issues multipart upload grants for S3-compatible storage.

The website is the user-facing catalog. Anonymous users can browse/search public
entries and open entry/model pages. Signed-in users can create entries with L0-L3
data, upload local files, reference URLs, import public Ext experiment files,
attach programs and metrics, and view model data through the structure-focused
UI.

## Data Model

The core graph follows the same structure as the specification:

```text
Entry
+-- Models
+-- Entities
`-- Entity Relations
```

An `Entry` represents one baseline source dataset. A `Model` groups related
processing or modeling work inside that entry. An `Entity` is an individual data
object, model file, metric set, or program record. `EntityRelation` rows connect
inputs, outputs, and evaluations with relation types such as `input_to`,
`output_of`, and `metrics_for`.

Entities use the L0-L3 maturity levels:

```text
L0 raw source data
L1 processed source data
L2 structural models
L3 model-to-data evaluations
```

## Local Development

Start Postgres and apply migrations:

```bash
cd backend
make start-postgres
make migrate-local
```

Build and run the backend API:

```bash
cd backend
make install-tools
make generate
make build
DYNAMIC_PDB_ENV=local ./bin/server
```

Run the website:

```bash
cd website
npm install
npm run dev
```

In development, the website defaults to `http://localhost:8080` for backend API
calls and the backend listens on `:8080`. Override the website API target with
`NEXT_PUBLIC_API_BASE_URL`. File uploads require S3 or S3-compatible credentials
configured through `backend/config/local.yml` or `DYNAMIC_PDB_*` environment
variables.

## Build and Test

Backend:

```bash
cd backend
make generate
make build
make test
```

Website:

```bash
cd website
npm run typecheck
npm test
npm run build
```

Repository lint:

```bash
make lint
```

CI runs backend generation, linting, database migrations, Go tests with coverage,
website typechecking, Node tests with coverage, and production image builds for
`main`.

## Where to Look First

- New to the project? Read [SPEC.md](SPEC.md) for the scientific model and
  intended workflows.
- Working on the API? Start with [`backend/api/openapi.yaml`](backend/api/openapi.yaml)
  and [`backend/internal/httpapi/`](backend/internal/httpapi/).
- Changing persistence? Look at [`backend/internal/db/`](backend/internal/db/)
  and [`backend/migrations/db/public/structure/`](backend/migrations/db/public/structure/).
- Working on the UI? Start with [`website/src/app/page.tsx`](website/src/app/page.tsx)
  and [`website/src/app/components/`](website/src/app/components/).
- Deploying? See [`deploy/README.md`](deploy/README.md).

