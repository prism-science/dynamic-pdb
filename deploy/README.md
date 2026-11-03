# dynamic-pdb deployment

This chart deploys the Dynamic PDB backend, website, Flyway migrations, the S3
proxy that serves CLI release artifacts, the MMseqs similarity job, the
artifact checksum job, the model compounds job, and the archive re-derivation job.

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

## Archive re-derivation job

The archive re-derivation job (T1-C2) fills in what the C1 migrations could not for archive
entries. From the first 4 MiB of each entry's RCSB mmCIF, the same bytes the entry sync reads,
it derives the entry's ligands, cofactors, their decision record (`compound_sources`) and the
glycan descriptors, and its polymer entities' mutations in UniProt numbering. It changes the
active revision in place, without a new revision, and logs the old and new values in
`archive_rederivations`, so `backend/migrations/inventory/revert_archive_rederivation.sql` can
put them back. It runs `/app/archive-rederive-job` from the backend image and uses only the
backend's database secret.

Lists a person set are kept: lists without a decision record, or marked `depositor` in
`field_sources`. Mutations a person typed are kept too. An entry with a revision in review is
skipped and tried again on the next run, since a draft made before the change would drop it when
it is activated. A file that cannot be downloaded is tried on three runs, then left `failed`. A
head that ends before the coordinates gives `skipped_incomplete_file` and is not tried again.

Each run takes pages of 200 revisions, downloads 4 files at a time, and writes each entry in its
own transaction. After each page it pauses as long as the page's writes took, so the database is
at most about half busy with this. Runs stop taking pages after 50 minutes and are stopped after
55; `maxEntriesPerRun` caps the revisions one run looks at (`0` means no cap). The job's
statements may run for `statementTimeout` (30 seconds) instead of the backend's 10.

It is off on every environment. The prod run starts with a dry run, which logs what would change
in rows marked `dry_run` and changes nothing else:

```yaml
archiveRederiveJob:
  enabled: true
  dryRun: true
  maxEntriesPerRun: 1000
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

On 2026-10-27 at 22:04 UTC prod's database stalled on WAL writes, and the job was paused. The
RDS metrics showed why: the instance, then a db.t4g.micro, had used up its EBS burst balance
(`EBSByteBalance%` and `EBSIOBalance%` fell from about 65% to 0 during that evening's fills).
Prod has run on a db.t4g.small since 22:58 UTC that day, with both balances at 100%, and the
first fill is done, so later runs only read `artifacts` to find missing rows.

Each hourly run stops claiming files after 50 minutes and is stopped after 55;
`maxFilesPerRun` caps the files one run checks (`0` means no cap). The job came back on
2026-10-30 with a cap of 5000. That first run took 9.5 minutes (about 530 files a minute),
found nothing missing in 7 seconds and raised the API's p95 from about 29 to 37 ms, so the cap
is now 15000: about 28 minutes a run, and the ~290,000 files left take about 20 runs. If
either EBS balance falls below 50% during a run, or the API slows down, lower the cap again:

```yaml
checksumJob:
  enabled: true
  maxFilesPerRun: 5000
```

To stop it at once, before a chart change reaches the cluster:

```sh
kubectl -n dynamicpdb patch cronjob dynamic-pdb-checksum-job -p '{"spec":{"suspend":true}}'
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
during a deploy). Archive models, whose files are on files.rcsb.org, get none until the archive
switches below are on; then the job reads the checksum job's copy of each file.

Model files stored before the job existed (on prod about 152,000 qFit and ensemble-refinement
models, 1.6 TB, from the August 2026 bulk import) get their sets only when
`modelCompoundsJob.historicalFill` is on. It only queues them: each run pages through every
artifact, 2,000 per statement, pausing as long as each page took, as the checksum job does, and
it repeats that scan on every run while it is on. It ran on 2026-11-02: the first run with it
(10:35 UTC) queued all 152,442 files in about 40 seconds, and it was turned off again. Runs
read 100 files each at first, in 26 to 31 seconds with no failures; on 2026-11-02 the cap went
to 500 files a run, about 6,000 an hour, so the ~145,000 files still queued take about a day.
On 2026-11-03, with about 20,000 left, the cap was removed (`maxFilesPerRun: 0`): a run then
read for all 4 claim minutes, about 880 files, and the queue was empty at 16:01 UTC. The cap is
back at 500. A run stops claiming files after 4 minutes, so a run of large files reads
fewer. To queue files
again, for example after another bulk import:

```yaml
modelCompoundsJob:
  historicalFill: true
```

A run's `model compounds job started` line shows `historical_sets_added`. Turn
`historicalFill` off as soon as a run shows 0; reading goes on from the queue. A run's log is gone once the next
run finishes (`successfulJobsHistoryLimit: 1`), so read it within 5 minutes or in Loki. Follow
the reading with:

```sql
select status, count(*) from modeled_compound_sets group by 1;
```

Prod's RDS has 20 GB of storage and does not grow by itself. If either RDS EBS balance falls
below 50% or free storage below 5 GB, stop the job with `modelCompoundsJob.enabled: false`,
and until that deploys:

```sh
kubectl -n dynamicpdb patch cronjob dynamic-pdb-model-compounds-job -p '{"spec":{"suspend":true}}'
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

### Archive model files

Archive models keep their file's URI on files.rcsb.org, but the checksum job copies each file
into our bucket. Two switches let the job read those copies, with the same reader as uploads
(T3-M2). They are in `backend/config/<env>.yml`, not in `values.yaml`, so dev can go first:
dev and prod share this chart's values.
Both went on for dev and prod on 2026-11-03. `archive_fill` is off again since its fill finished;
`archive_files` stays on.

```yaml
compounds:
  archive_files: true  # each run's 48-hour window also takes archive files once they are copied
  archive_fill: false  # when on, each run tops the queue up to maxFilesPerRun with older copies
```

- `archive_files` stays on. New archive files (a new entry, a weekly sync, a T3-C coordinate
  file) are queued by the first run after the hourly checksum job has copied them.
- `archive_fill` catches up on files copied before `archive_files` was on (about 73,000 on prod).
  It needs `archive_files` and a `maxFilesPerRun`. Each run tops the queue up to
  `maxFilesPerRun`, never more, so an upload saved during the fill waits about one run. Like the
  historical fill, each run pages through every artifact from the start, 2,000 per statement,
  pausing as long as each page took. At 500 files a run the fill takes about 12 to 13 hours;
  at `maxFilesPerRun: 1500` a run reads as many as its 4 claim minutes allow (about 1,150
  archive files), about 5 hours for prod's ~73,000.
  Turn it off, and `maxFilesPerRun` back to 500, once a run's `started` line shows
  `archive_sets_added=0` and nothing is pending.
- A file the checksum job kept no copy of (`over_limit`, `mismatch`, `copy_failed`,
  `copy_off`) gets no set: the API reports `not_stored`, as before.
- A run's `model file read` lines say `read_from=checksum_copy` for archive files.
- The job never changes a model revision. The page shows the file's compounds once a set is
  done, and keeps RCSB's `nonpolymer_bound_components` (the model's `ligands`) until then.

The stop rules above apply. Switching `archive_files` and `archive_fill` off stops new archive
sets; the few already queued are still read. To take the archive sets out again (the API answers
`not_stored` at once; search keeps the file's compound IDs until each entry is indexed again):

```sql
begin;
create temp table archive_sets on commit drop as
  select sets.artifact_id
  from modeled_compound_sets sets
  join artifacts on artifacts.id = sets.artifact_id
  where artifacts.uri like 'https://files.rcsb.org/%';
delete from modeled_compound_records where artifact_id in (select artifact_id from archive_sets);
delete from modeled_compound_sets where artifact_id in (select artifact_id from archive_sets);
commit;
```

One transaction is enough: 1,000 sets took 0.05 s on the prod copy, so all ~73,000 take seconds.
The parser runs (`runs` named `modeled compound read`) stay. Nothing points at them once their
sets are gone, and deleting them is slow: `metrics.run_id` and `model_revision_runs.run_id` have
no index, so each deleted run scans both tables (1,000 runs took 49 s on the prod copy).
