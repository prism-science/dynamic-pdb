# dynamic-pdb deployment

This chart deploys the Dynamic PDB backend, website, Flyway migrations, the S3
proxy that serves CLI release artifacts, and the MMseqs similarity job.

## Environments and secrets

One Helm chart and one ApplicationSet deploy both environments. Environment
differences are values in the ApplicationSet matrix; there are no copied
templates or values files.

| Environment | Branch | Release | Namespace | Host | Image tag |
| --- | --- | --- | --- | --- | --- |
| production | `main` | `dynamic-pdb` | `dynamicpdb` | `dynamicpdb.com` | `sha-<commit>` |
| dev | `dev` | `dev-dynamic-pdb` | `dev-dynamicpdb` | `dev.dynamicpdb.com` | `dev-sha-<commit>` |

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
MMseqs job images for `main` and `dev`. Production images use `sha-<commit>` and
`latest`; dev images use `dev-sha-<commit>` and `dev-latest`. The ApplicationSet
resolves each branch head and deploys the matching immutable tag, so CI does
not commit image-tag changes back to git. The `dev` branch must exist before
the dev generator can resolve its first revision.

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
