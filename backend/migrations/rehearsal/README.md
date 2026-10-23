# Migration rehearsal

Run a branch's database migrations on a temporary copy of production before you
merge it. Merging to `main` runs migrations on the production database
automatically (`deploy/templates/migration-job.yaml`, a Helm pre-upgrade hook),
so this is the last place to catch a problem.

## Each rehearsal

From `backend/`:

```bash
make rehearsal-up                                      # ~15-20 min: restore production into dynamic-pdb-rehearsal
make rehearsal-run BRANCH=origin/lh/schema-migrations  # ~5-10 min: run that branch's migrations, write a report
make rehearsal-down                                    # ~5 min: delete the copy
```

`make rehearsal-status` shows what exists right now.

`rehearsal-run` prints the path of `report.md` when it finishes. Paste that file
into the PR. Pass `origin/<branch>` to test exactly what is pushed; any git ref
works, including a local branch or a commit.

Use one copy per branch. A second run on the same copy starts from the first
run's results, so the report flags it and asks for a fresh copy.

## Reading the report

| Verdict | Meaning |
|---|---|
| PASS | Every migration applied, a second run did nothing, no rows disappeared, no broken indexes. |
| CHECK | The migrations applied, but something needs your judgment: rows disappeared, a table is gone, the branch's `inventory/counts.sql` changed, or nothing was pending. |
| FAIL | A migration failed, an index was left invalid, or a second run applied something again. `rehearsal-run` exits non-zero. |

"Tables that changed" lists every table whose rows or values differ.
"values changed" is expected for tables a data migration rewrites; compare the
list with what the PR says it changes.

The timings come from a copy with no other traffic. On production, a migration
that waits for a lock can take longer.

The report folder (`.tmp/rehearsals/<time>-<branch>/`, ignored by git) also
keeps the Flyway logs, the before and after snapshots, and `source/`, the exact
migrations that ran.

## How it works

```
prod RDS dynamic-pdb ── point-in-time restore ──► dynamic-pdb-rehearsal  (same version, size, network)
   (never changed)                                    ▲
                                                      │ tunnel pod in the dynamicpdb namespace
your machine: rehearsal-run ──► port-forward ─────────┘
  1. build the branch's migrations image from its own Dockerfile, as the deploy does
  2. snapshot every table: row count and checksum
  3. flyway migrate, then flyway migrate again (it must do nothing)
  4. snapshot again, compare, check for invalid indexes and failed migrations
  5. if the branch has inventory/counts.sql or inventory/schema_inventory.sql, run them too
  6. write report.md
```

## Safety rails

- Production is only ever the source of the restore. Every command that changes
  or deletes an instance refuses names that do not start with
  `dynamic-pdb-rehearsal`, and instances without the
  `purpose=migration-rehearsal` tag.
- The copy gets a new random password right after the restore, so a leaked
  rehearsal password cannot open production. The password lives in the secret
  `dynamicpdb/dynamic-pdb-rehearsal-db`, and `rehearsal-down` deletes it.
- No dump of production data is written to your machine. The `users` table holds
  email addresses.
- Each copy carries an `expires-at` tag 4 hours out. A forgotten copy costs about
  $12 a month; `make rehearsal-status` lists any that exist.
- The tunnel pod is deleted at the end of every run and stops itself after 4 hours.

## Setup (once)

1. **Cluster access.** Membership in the GitHub team `Astera-org/dynamicpdb`
   gives edit rights in the `dynamicpdb` namespace. Create a kubeconfig with
   `astera-k3s/generate-user-kubeconfig.sh` (needs `kubelogin`). Set
   `KUBE_CONTEXT` if that context is not your current one.
2. **AWS access.** By default the script reads the rehearsal user's key from the
   secret `dynamicpdb/dynamic-pdb-rehearsal-aws` (created by astera-k3s). If you
   have your own AWS identity with the same permissions, set `AWS_PROFILE`
   instead.
3. **Tools:** `aws`, `kubectl`, `jq`, `docker`, `psql`, `openssl`, `git`.

## Practice copy on your machine

No access needed. It copies your local dev database (`dynamic-pdb-postgres-1`)
into Postgres 18.3, the production version, and rehearses against that:

```bash
make rehearsal-local-up
make rehearsal-run TARGET=local BRANCH=origin/lh/schema-migrations
make rehearsal-local-down
```

It catches SQL that fails on Postgres 18 or on your test data. It says nothing
about production data or timings. It uses the `data/local` Flyway location, like
`make migrate-local`, instead of production's `data/prod`.

## Settings

| Variable | Default | Use |
|---|---|---|
| `REHEARSAL_INSTANCE` | `dynamic-pdb-rehearsal` | A second copy at the same time, for example `dynamic-pdb-rehearsal-2` |
| `KUBE_CONTEXT` | current context | Which kubeconfig context to use |
| `REHEARSAL_NAMESPACE` | `dynamicpdb` | Namespace of the secrets and the tunnel pod |
| `REHEARSAL_TUNNEL_PORT` | `45432` | Local port of the tunnel |
| `REHEARSAL_LOCAL_PORT` | `45433` | Local port of the practice copy |
| `REHEARSAL_TUNNEL_IMAGE` | `harbor.astera.sh/dockerhub-cache/alpine/socat:1.8.1.1` | Image of the tunnel pod |
| `AWS_PROFILE` | unset | Your own AWS identity instead of the rehearsal user's key |

## Troubleshooting

- **`rehearsal-up` stops after 60 minutes.** Run it again; it continues with the
  existing copy.
- **A migration failed halfway.** The copy is now in the state production would
  be in. Read `flyway-migrate.log` in the report folder, fix the branch, then
  `make rehearsal-down` and `make rehearsal-up` for a fresh copy.
- **AWS refuses `--delete-automated-backups` in `rehearsal-down`.** The
  rehearsal user also needs `rds:DeleteDBInstanceAutomatedBackup`; ask in
  astera-k3s.
- **The port is already in use.** Set `REHEARSAL_TUNNEL_PORT` or
  `REHEARSAL_LOCAL_PORT`.
