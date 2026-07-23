# dynamic-pdb deployment

This chart deploys the Dynamic PDB backend, website, and Flyway migrations.

## Namespace and secrets

Create the runtime secrets in the Kubernetes namespace targeted by the ArgoCD
ApplicationSet.

Required secrets:

- `dynamic-pdb`: created by `astera-k3s`, with `host`, `port`, `database`, `username`, `password`, and `url`.
- `dynamic-pdb-s3-data`: created by `astera-k3s`, with `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`, and `S3_BUCKET`.
- `dynamic-pdb-backend`: create manually or from infra, with `jwt-secret` and `github-client-secret`.

Example app secret:

```sh
kubectl -n <namespace> create secret generic dynamic-pdb-backend \
  --from-literal=jwt-secret="$(openssl rand -hex 32)" \
  --from-literal=github-client-secret="<github-oauth-client-secret>"
```

## ArgoCD

Apply once:

```sh
kubectl apply -f deploy/appset.yaml
```

The GitHub Actions workflow builds and pushes backend, migrations, and website
images as `sha-<commit>` on `main`. The ApplicationSet resolves the current
`main` SHA and deploys the chart with those image tags, so CI does not commit
image-tag changes back to git.

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
