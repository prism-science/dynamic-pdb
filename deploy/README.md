# dynamic-pdb deployment

This chart deploys the Dynamic PDB backend, website, Flyway migrations, the S3
proxy that serves CLI release artifacts, the MMseqs similarity job, the
artifact checksum job, and the model compounds job.

## Environments and secrets

One Helm chart and one ApplicationSet deploy both environments. Environment
differences are values in the ApplicationSet matrix; there are no copied
templates or values files.

| Environment | Branch | Release | Namespace | Host | Image tag |
| --- | --- | --- | --- | --- | --- |
| production | `main` | `dynamic-pdb` | `dynamicpdb` | `dynamicpdb.com` | `sha-<commit>` |
| dev | `main` | `dev-dynamic-pdb` | `dev-dynamicpdb` | `dev.dynamicpdb.com` | `dev-sha-<commit>` |

`astera-k3s` creates each namespace and three release-derived secrets:

- `<release>`: database connection fields.
- `<release>-s3-data`: credentials and bucket name for environment data.
- `<release>-s3proxy-aws`: read-only credentials for the environment's CLI release bucket.

The app secret `<release>-backend`, containing `jwt-secret` and
`github-client-secret`, is created manually in each namespace. Production keeps
its existing names; dev uses the same convention with the `dev-` prefix.
The GitHub OAuth app behind the configured client ID must list both exact
callback URLs: `https://dynamicpdb.com/auth/github/callback` and
`https://dev.dynamicpdb.com/auth/github/callback`.

Example app secret:

```sh
kubectl -n dynamicpdb create secret generic dynamic-pdb-backend \
  --from-literal=jwt-secret="$(openssl rand -hex 32)" \
  --from-literal=github-client-secret="<github-oauth-client-secret>"

kubectl -n dev-dynamicpdb create secret generic dev-dynamic-pdb-backend \
  --from-literal=jwt-secret="$(openssl rand -hex 32)" \
  --from-literal=github-client-secret="<github-oauth-client-secret>"
```

## ArgoCD

Apply once:

```sh
kubectl apply -f deploy/appset.yaml
```

The GitHub Actions workflow builds backend, migrations, website, CLI, and
MMseqs job images from `main`. Production images use `sha-<commit>` and
`latest`; dev images use `dev-sha-<commit>` and `dev-latest`. Both ApplicationSet
entries resolve the same `main` branch head and deploy environment-specific
tags for that commit, so CI does not commit image-tag changes back to git.

## MMseqs job

The MMseqs job is deployed as a Kubernetes CronJob. It uses the same database
secret as the backend and stores its local similarity index under a persistent
volume mounted at `/app/.tmp/mmseqs`.

The MMseqs job runs once per day:

```yaml
mmseqsJob:
  enabled: true
  schedule: "0 0 * * *"
  timeZone: Etc/UTC
```

The chart creates a PVC for the MMseqs cache. The default values expect an `ebs-gp3`
storage class and request `16Gi`; override `mmseqsJob.cache.storageClassName`
and `mmseqsJob.cache.size` if the cluster uses different storage.

To run it manually:

```sh
kubectl -n dynamicpdb create job --from=cronjob/dynamic-pdb-mmseqs-job dynamic-pdb-mmseqs-job-manual-$(date +%s)
```

For dev, use namespace `dev-dynamicpdb` and CronJob
`dev-dynamic-pdb-mmseqs-job`.

## Infrastructure prerequisites

`astera-k3s` provides the shared `dynamicpdb.com` wildcard edge and isolated
state for both environments:

- Route 53 hosted zone lookup or records for `dynamicpdb.com`.
- ACM certificate and ALB listener coverage for `dynamicpdb.com` and
  `dev.dynamicpdb.com`.
- A route from that ALB to the shared Traefik public NodePort.
- Separate production/dev RDS instances, data buckets, IAM users, Kubernetes
  secrets, and CloudFront distributions (`files.dynamicpdb.com` and
  `dev-files.dynamicpdb.com`).

Each data bucket allows browser multipart uploads from its application origin:

- production origin: `https://dynamicpdb.com`
- dev origin: `https://dev.dynamicpdb.com`
- allowed methods: `GET`, `HEAD`, `PUT`
- allowed headers: `*`
- exposed headers: `ETag`

## CLI releases

Tag pushes matching `v*` run `.github/workflows/release.yml`. The workflow
cross-compiles the CLI, uploads immutable archives and `checksums.txt` under
`releases/<tag>/` in the production and dev release buckets, then updates
`releases/latest.txt`, `install.sh`, and `config/dynamic-pdb.yaml` in both.

The chart routes `/install.sh`, `/releases/*`, and `/config/*` to s3proxy, so
each environment serves the same immutable release artifacts from its own
bucket and read-only credentials. Users can install with:

```sh
curl -fsSL https://dynamicpdb.com/install.sh | bash
```

## Checksum job

The checksum job computes the sha256 and size of every artifact's bytes and records the
result in `artifact_checksums`: `verified`, `mismatch` or `failed`. It keeps a copy of each
verified external file (such as an RCSB file) up to `copyLimitBytes` in the data bucket. It runs
`/app/checksum-job` from the backend image, so it needs no image of its own, and it uses the
backend's database and `-s3-data` secrets.

Each verified or mismatch row also records its copy status, which the API returns as
`checksum.copy_status`. Pending and failed rows have none.

| Copy status | Meaning |
|---|---|
| `copied` | Our copy of the external file is at `stored_uri`. |
| `own_storage` | The file is in our bucket already, so it needs no copy. |
| `over_limit` | The external file is larger than `copyLimitBytes`. It is still hashed; only its link is kept. |
| `mismatch` | The bytes differ from the declared sha256 or size, so the copy was discarded. |
| `copy_failed` | The copy could not be started or stored. The reason is in the job's log; the row is not checked again. |
| `copy_off` | Copying external files is turned off (`checksum::copy_external: false`). |

A URL that answers with an HTML page, such as the landing page a DOI resolves to, is `failed`
at once with "landing page, not a file". The job takes a response as a page when its
Content-Type is `text/html` or `application/xhtml+xml`, or when its body starts with
`<!doctype html` or `<html`. The real files send other types: `chemical/x-cif` from
`files.rcsb.org/download`, `text/x-fasta` from `www.rcsb.org/fasta`. Files in our own bucket
are read from S3, not over HTTP.

The copy limit is `checksumJob.copyLimitBytes`, 1 GiB (`1073741824`), the same as the upload
limit. The researchers asked for 2 GB on 2026-10-29 and have not yet said whether they mean it
(question A). Their answer changes only this value: 1 GB is `1000000000`, 2 GB is `2000000000`
and 2 GiB is `2147483648`. A new limit applies to files checked from then on; files already
recorded as `over_limit` keep that status.

The migration that added the copy status (2026-10-29) filled it in for the rows already checked.
Verified external files without a copy, whose reason was never stored (a few hundred on prod),
went back to pending, so the next run reads them again and records why.

Each run first adds a pending row for every artifact that has none, 2000 artifacts per
statement, and after each statement pauses as long as it took, so the database is at most
about half busy with this. The first run does this for all of prod's ~568,000 artifacts. As
one statement it ran over 5 minutes on prod and slowed the API, so it must stay in pages.
The job's statements may run for `statementTimeout` (30 seconds) instead of the backend's 10.

It is paused: on 2026-10-27 at 22:04 UTC prod's database stalled on WAL writes after a day of
heavy checksum writes, and the job stays off until the RDS metrics are understood. Each hourly
run stops claiming files after 50 minutes and is
stopped after 55; `maxFilesPerRun` caps the files one run checks (`0` means no cap). The
rollout starts with a cap of 1000; set it to `0` once the first runs and the S3 bill look
right:

```yaml
checksumJob:
  enabled: true
  maxFilesPerRun: 1000
```

To run it manually:

```sh
kubectl -n dynamicpdb create job --from=cronjob/dynamic-pdb-checksum-job dynamic-pdb-checksum-job-manual-$(date +%s)
```

## Model compounds job

The model compounds job reads every model file uploaded to our bucket and stores the compounds
it models (every copy outside the polymer chains, water excepted) in
`modeled_compound_records`, one set per artifact in `modeled_compound_sets`
(`lib/cif` `ReadModeledCompounds`, T3-M). It runs `/app/model-compounds-job` from the backend
image every 5 minutes and reads one file at a time, so a large file never takes CPU from the
API pods. A new upload gets a pending set when its artifact is created; each run also gives
one to any model file of the last 48 hours that has none (an upload an older backend stored
during a deploy). Archive models, whose files are on files.rcsb.org, get none.

Model files stored before the job existed (on prod about 152,000 qFit and ensemble-refinement
models, 1.6 TB, from the August 2026 bulk import) get their sets only when
`modelCompoundsJob.historicalFill` is on. Each run then pages through every artifact, 2,000 per
statement, pausing as long as each page took, as the checksum job does. Turn it on after a quiet
day with a cap, then raise the cap once RDS metrics and the job's logs look right:

```yaml
modelCompoundsJob:
  historicalFill: true
  maxFilesPerRun: 100
```

A file is retried after 10 and 20 minutes, then marked `failed` with the reason in
`last_error`; so is a file whose read ended the job three times (out of memory or time). Job alerts are muted for this namespace, so check failures with:

```sql
select artifact_id, attempts, last_error from modeled_compound_sets where status = 'failed';
```

To read a file again (for example after a parser change), set its set back to pending:

```sql
update modeled_compound_sets set status = 'pending', attempts = 0, next_attempt_at = now()
where artifact_id = '<artifact id>';
```

To run it manually:

```sh
kubectl -n dynamicpdb create job --from=cronjob/dynamic-pdb-model-compounds-job dynamic-pdb-model-compounds-job-manual-$(date +%s)
```
