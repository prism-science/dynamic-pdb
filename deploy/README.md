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

It is on only for a planned run, and off otherwise. Prod's run went through all 73,074 archive
revisions on 2026-11-03, in four hourly runs, and the job is off again. A prod run starts with a
dry run, which logs what would change in rows marked `dry_run` and changes nothing else:

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

## Coordinate index job

The coordinate index job reads every model file in our bucket, and the checksum job's copy of
each archive model file, and stores one row per author chain of the file's first model in
`coordinate_index_chains`, one set per artifact in `coordinate_index_sets` (`lib/cif`
`ReadCoordinateIndex`, S2): the chain's entity, its deposited sequence length and where that
came from (`length_source`), its modeled residues, and whether it is a protein chain, with the
test that decided (`protein_source`: the file's `_entity_poly.type`, else the residue majority).
The model search's protein chain filters (`protein_chains_min`, `protein_chain_length_min`,
`protein_chain_length_max`, `protein_chain_mode` on `GET /v1/models`) read them. It runs `/app/coordinate-index-job`
from the backend image every 5 minutes, one file at a time, like the model compounds job.

It is merged off (`coordinateIndexJob.enabled: false`). A new upload still gets a pending set
when its artifact is created, so nothing is missed until it runs; each run also gives one to any
model file of the last 48 hours that has none, and to archive files whose copy the checksum job
recorded in that window, however old their artifact (`coordinate_index.archive_files` in
`backend/config/<env>.yml`, on for dev and prod).

A chain whose length the file does not state (`_entity_poly_seq` in archive mmCIF, `SEQRES` in
PDB format) takes it from the entry's FASTA record that names it (`Chains A, B[auth C]`) as
author chain, else as label chain. The job reads those records in the transaction that saves
the chains. When an activation changes the entry's FASTA records (adds or removes some), the API
queues the entry's model files again, ahead of the fill, in the same transaction. Each set records
the entries its read used (`fasta_entry_ids`); each run queues a done file again when the entries
of the models using it are no longer these (a file read before its model existed, a new model of
another entry, a model revision that switched to the file), looking at model revisions of the last
48 hours.

### Rollout

1. Switch-on PR: `coordinateIndexJob.enabled: true` and `coordinateIndexJob.fill: true`. The
   first run queues every existing model file as a fill row (about 225,000 on prod, in pages of
   2,000 artifacts, about 40 seconds). Later runs add nothing while fill rows are pending
   (`fill_waiting_for_pending=true` in the `started` line). Uploads are always read before fill
   rows. At 500 files a run the fill takes about 1 to 1.6 days.
2. Fill-off PR: `fill: false` once a run logs `fill_sets_added=0` and no fill row is pending:

   ```sql
   select fill, status, count(*) from coordinate_index_sets group by 1, 2 order by 1, 2;
   ```

3. Chain filters PR: `model_search.chain_filters_enabled: true` in `backend/config/<env>.yml`.
   Until then a chain parameter answers 400, so a search never silently misses a file that is
   not read yet.

The chain table holds about 2 rows per file (about 0.42 million rows, 55 MB on prod, plus about
25 MB of sets). Watch RDS free storage and the EBS balances as for the model compounds job; stop
the job with `coordinateIndexJob.enabled: false`, and until that deploys:

```sh
kubectl -n dynamicpdb patch cronjob dynamic-pdb-coordinate-index-job -p '{"spec":{"suspend":true}}'
```

### Conformer counts

Each read can also store the file's conformer counts (`ReadCoordinateIndex`, S3), for the
residue filter of researcher example search 1 ("a lysine with more than three conformations"):
one row per residue type and conformer count in `coordinate_index_residue_counts`, with the
file's totals on its set (`residues`, `multi_conformer_residues`).

- A residue is label chain + label residue number + residue type. Where the file has no label
  numbering (PDB format), the job maps author chain + number + insertion code + type to label
  numbering through the entry's deposited mmCIF (`_pdbx_poly_seq_scheme`, read from our copy of
  the entry's active Deposited model's file, once per run); residues no mapping covers stay keyed
  by author numbering, with a warning on the set saying why (`residue_key`: `label`, `mapped`,
  `author`, `mixed`). Two residue types at one position stay two residues. Mapping renames
  residues. A mapped residue merges with the file's own label rows only when those rows give the
  same author numbering (at most 16 letters together; more are left out with a warning); where the
  deposited numbering names a different residue of the file, it keeps its author key, with a
  warning.
- The model's `altloc_fraction` (upload extraction, API and CLI, and the website preview) uses
  the same residues and rule: residues with 2 or more conformers ÷ residues.
- Its conformer count is the number of its altloc letters whose highest heavy-atom occupancy in
  the residue is at least 0.1, minimum 1. Letters only on hydrogens never count.
- Ensembles get no counts (`conformers`): a file with more than one model frame
  (`ensemble_model_frames`), or one whose model's latest revision using it is an Ensemble or has
  `member_count` (`ensemble_model_type`). While counts are on, each run reads a file again when a
  new revision changes that, and re-checks every file stored as `ensemble_model_type`, so a
  rejected Ensemble revision never leaves its file without counts. A counted file that a model starts using as an
  Ensemble keeps its counts until its next read; the residue filter leaves out Ensemble models by
  their type.

They are merged off: `coordinate_index.residue_counts: false` in `backend/config/<env>.yml`
(on locally), and `coordinateIndexJob.residueFill: false`. Off, a read stores chains only and keeps
any counts an earlier read stored.

Rollout, once the job runs (`coordinateIndexJob.enabled`) and its own fill is done:

1. Switch-on PR: `residue_counts: true` in `backend/config/dev.yml` and `production.yml`, and
   `coordinateIndexJob.residueFill: true`. The first run sends every done set without current
   counts back to pending as a fill row (about 225,000 on prod, pages of 2,000), and reads them
   after uploads. Failed sets are left as they are. A set queued again keeps its chains, so the
   chain filter never loses a model meanwhile. At 500 files a run this takes 1 to 1.6 days.
2. Fill-off PR: `residueFill: false` once a run logs `residue_fill_sets_requeued=0` and none are
   pending:

   ```sql
   select conformers, residues_version, status, count(*) from coordinate_index_sets group by 1, 2, 3 order by 1, 2, 3;
   ```

3. Residue filters PR (S4): `model_search.residue_filters_enabled: true` in `backend/config/<env>.yml`,
   with the website's residue controls. Until then `residue_type`, `residue_conformers_min` and
   `residues_min` on `GET /v1/models` (and in saved selections) answer 400, so a search never
   silently misses a file that is not counted yet.

The counts take about 866 MB with their index for all models (5.75 million rows, S3 spike), and
the fill writes about 70 rows a second: watch RDS free storage, WAL and the EBS balances. The
residue-level altloc fraction of a file is `multi_conformer_residues / residues`.

### Failures and reading again

A file is retried after 10 and 20 minutes, then marked `failed` with the reason in
`last_error`; so is a file whose read ended the job three times, and at once a file that is not
a structure file or is over a reader limit. Job alerts are muted for this namespace, so check:

```sql
select artifact_id, attempts, last_error from coordinate_index_sets where status = 'failed';
```

A file queued again keeps the chains of its last read until the new read replaces them, so the
search never loses it in between. To read a file again (for example after a reader change that
bumps `cif.CoordinateIndexVersion`):

```sql
update coordinate_index_sets set status = 'pending', attempts = 0, next_attempt_at = now()
where artifact_id = '<artifact id>';
```

To run it manually:

```sh
kubectl -n dynamicpdb create job --from=cronjob/dynamic-pdb-coordinate-index-job dynamic-pdb-coordinate-index-job-manual-$(date +%s)
```

## Archive worker

The archive worker builds bulk-download archives (B2) and their previews (L1). `POST /v1/downloads`
adds a queued row to `download_jobs`; the worker (`/app/archive-worker` from the backend image,
Deployment `<release>-archive-worker`) runs two lanes, each one job at a time, so a long build
never holds up a preview:

1. **Planning lane.** It pages through the frozen selection's pinned models and files (100 models
   per read), lists them as the manifest will, and records the preview: record and file counts
   by type, availability and reason, checksum status, sizes (exact, estimate or unknown) and
   related datasets. It adds up the bytes of the files the zip can include: only files whose
   checksum result is `verified` with copy status `own_storage` or `copied`, whose bytes we hold
   and match the record. Over `downloads::max_archive_bytes` (10 GB) the job is `blocked`, with
   its preview, before any file is read and with nothing written to S3. Otherwise it is
   `planned`: a request with `prepare: false` (the website) waits there until
   `POST /v1/downloads/{id}/prepare`; one with `prepare: true` (the default) goes on.
2. **Building lane.** It plans the job again (a file copied since the preview replaces the
   preview, so the job always reports what its archive holds), then writes it: streams one zip
   into the data bucket by multipart upload, at
   `downloads/<job id>/<attempt>/dynamic-pdb-<selection id>-<file choice>.zip` (a folder per
   attempt, so an attempt that lost its lease never overwrites a later one's objects). Each
   included file is first
   copied to the scratch disk while its SHA-256 is computed; only a file that matches its
   recorded SHA-256 and size goes into the zip, and one that does not is listed as unavailable
   and logged as an ERROR. A file still unreadable after `archive_worker::file_read_attempts` (3)
   is listed as unavailable with reason `read_failed` and the archive goes on; only when
   `archive_worker::max_consecutive_read_failures` (5) files fail in a row is the bucket taken for
   down and the whole attempt tried again. `README.txt` comes first and `manifest.tsv` last (its
   `reason_code` column says why a file is not included); the manifest is also stored on its own
   at `downloads/<job id>/<attempt>/manifest.tsv`.
3. **Finish.** The building lane marks it `ready` with the archive's size, SHA-256, URLs and
   counts, offered for `downloads::archive_ttl` (7 days), and, in the same transaction, writes a
   `download_failures` row for each file it listed as unavailable or changed-at-source (L2).
   `GET /v1/downloads/{id}/failures` lists them; `POST /v1/downloads/{id}/retry` builds a second,
   small archive of the `read_failed` ones only (a job whose `retry_of` names the first; its
   README says so). Its top folder is `dynamic-pdb-<selection id>-retry-<its id>`, so unpacking it
   next to the first replaces nothing there. A write error on the worker's scratch disk (full, for
   example) is not a `read_failed` file: it fails the attempt, which is tried again.

The worker reads only our bucket; files we hold no matching copy of (over 1 GiB, changed at the
source, not copied) are listed in the manifest with their source URL, never fetched from RCSB.

**Leases.** A claim holds a job for `archive_worker::lease` (10 minutes) and a heartbeat extends
it every minute. A pod that dies frees its job when the lease runs out; a pod that is stopped (a
deploy) aborts its upload and puts the job back in its lane's queue at once (`queued` or
`planned`). A failed attempt, or a lease that ran out, is tried again, up to
`archive_worker::max_attempts` (3) in each lane (`failed_attempts` starts again at 0 once a job is
planned), then the job is `failed`. One read of one file may take
`archive_worker::file_timeout` (20 minutes) before it is tried again, and one attempt
`archive_worker::job_timeout` (4 hours), so a stalled read never holds the queue while the heartbeat
keeps the lease.

**Storage.** The worker uses the backend's `-s3-data` secret: its IAM user can already write and
abort multipart uploads. The `expire-downloads` lifecycle rule in astera-k3s deletes objects under
`downloads/` after 8 days (S3 rounds to midnight, so every archive lives its full 7 days); the
existing `abort-incomplete-multipart` rule removes uploads a crashed pod left. Nothing else in
the bucket is touched.

**Switch-on** is its own PR, after the lifecycle rule exists and selections are on:
`downloads::enabled: true` in the backend config and `archiveWorker.enabled: true` here.

To stop it at once, before a chart change reaches the cluster:

```sh
kubectl -n dynamicpdb scale deploy dynamic-pdb-archive-worker --replicas=0
```

To read a job (read-only, through the tunnel):

```sql
select id, status, prepare_requested, retry_of, file_choice, attempts, failed_attempts,
       planned_bytes, archive_bytes, files_included, files_linked, files_unavailable,
       files_changed_at_source, files_retryable, blocked_reason, error, created_at, finished_at,
       expires_at
from download_jobs order by created_at desc limit 20;

select reason_code, retryable, count(*) from download_failures
where job_id = '<job id>' group by 1, 2 order by 1;
```
