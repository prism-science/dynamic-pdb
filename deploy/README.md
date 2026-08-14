# dynamic-pdb deployment

This chart deploys the Dynamic PDB backend, website, Flyway migrations, the S3
proxy that serves CLI release artifacts, and the MMseqs similarity job.

## Namespace and secrets

This chart deploys into the `dynamicpdb` Kubernetes namespace. The namespace is
owned by `astera-k3s`; apply the infra change before syncing the ArgoCD
ApplicationSet.

Required secrets:

- `dynamic-pdb`: created by `astera-k3s` in `dynamicpdb`, with `host`, `port`, `database`, `username`, `password`, and `url`.
- `dynamic-pdb-s3-data`: created by `astera-k3s` in `dynamicpdb`, with `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`, and `S3_BUCKET`.
- `dynamic-pdb-s3proxy-aws`: created by `astera-k3s` in `dynamicpdb`, with `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` for read-only access to the CLI release bucket.
- `dynamic-pdb-backend`: create manually in `dynamicpdb`, with `jwt-secret` and `github-client-secret`.

Example app secret:

```sh
kubectl -n dynamicpdb create secret generic dynamic-pdb-backend \
  --from-literal=jwt-secret="$(openssl rand -hex 32)" \
  --from-literal=github-client-secret="<github-oauth-client-secret>"
```

## ArgoCD

Apply once:

```sh
kubectl apply -f deploy/appset.yaml
```

The GitHub Actions workflow builds and pushes backend, migrations, website, and
MMseqs job images as `sha-<commit>` on `main`. The ApplicationSet resolves the
current `main` SHA and deploys the chart with those image tags, so CI does not
commit image-tag changes back to git.

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

## Infrastructure prerequisites

`dynamicpdb.com` still needs public edge infrastructure in `astera-k3s`:

- Route 53 hosted zone lookup or records for `dynamicpdb.com`.
- ACM certificate and ALB listener coverage for `dynamicpdb.com`.
- A route from that ALB to the shared Traefik public NodePort.

The `dynamic-pdb-data` bucket also needs S3 CORS for browser multipart uploads:

- allowed origin: `https://dynamicpdb.com`
- allowed methods: `GET`, `HEAD`, `PUT`
- allowed headers: `*`
- exposed headers: `ETag`

## CLI releases

Tag pushes matching `v*` run `.github/workflows/release.yml`. The workflow
cross-compiles the CLI, uploads immutable archives and `checksums.txt` under
`s3://dynamic-pdb-dynamicpdb-com/releases/<tag>/`, then updates
`releases/latest.txt`, `install.sh`, and `config/dynamic-pdb.yaml`.

The chart routes `/install.sh`, `/releases/*`, and `/config/*` to s3proxy, so
users can install with:

```sh
curl -fsSL https://dynamicpdb.com/install.sh | bash
```
