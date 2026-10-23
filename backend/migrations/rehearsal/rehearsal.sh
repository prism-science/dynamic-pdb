#!/usr/bin/env bash
# Rehearse a branch's database migrations on a temporary copy of production.
# See README.md next to this file. Usually run through the Makefile targets.
#
#   up                                  restore production's latest state into the rehearsal RDS instance
#   run --branch REF [--target aws|local]
#                                       run REF's migrations on the copy and write a report
#   down                                delete the rehearsal RDS instance and its password secret
#   status                              show which rehearsal resources exist
#   local-up | local-down               a practice copy of your local dev database on Postgres 18.3
#
# Production is only ever the source of a point-in-time restore. Every command that
# changes or deletes an RDS instance first checks that its name starts with
# dynamic-pdb-rehearsal and that it carries the purpose=migration-rehearsal tag.
# Written for the bash 3.2 that ships with macOS.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)"
REPORTS_DIR="$REPO_ROOT/.tmp/rehearsals"

SOURCE_INSTANCE="dynamic-pdb"
PROTECTED_INSTANCES="dynamic-pdb dev-dynamic-pdb"
REHEARSAL_INSTANCE="${REHEARSAL_INSTANCE:-dynamic-pdb-rehearsal}"
REHEARSAL_TAG="migration-rehearsal"
EXPIRY_HOURS=4
AWS_REGION="${AWS_REGION:-us-west-1}"
export AWS_REGION

NAMESPACE="${REHEARSAL_NAMESPACE:-dynamicpdb}"
AWS_SECRET_NAME="dynamic-pdb-rehearsal-aws"
DB_SECRET_NAME="dynamic-pdb-rehearsal-db"
TUNNEL_POD="dynamic-pdb-rehearsal-tunnel"
TUNNEL_IMAGE="${REHEARSAL_TUNNEL_IMAGE:-harbor.astera.sh/dockerhub-cache/alpine/socat:1.8.1.1}"
TUNNEL_PORT="${REHEARSAL_TUNNEL_PORT:-45432}"
KUBE_ARGS=(--namespace "$NAMESPACE")
if [ -n "${KUBE_CONTEXT:-}" ]; then
	KUBE_ARGS=(--context "$KUBE_CONTEXT" "${KUBE_ARGS[@]}")
fi

LOCAL_CONTAINER="dynamic-pdb-rehearsal-local"
LOCAL_IMAGE="postgres:18.3"
LOCAL_PORT="${REHEARSAL_LOCAL_PORT:-45433}"
LOCAL_DB="dynamic_pdb_rehearsal"
LOCAL_USER="postgres"
LOCAL_PASSWORD="password"
DEV_CONTAINER="dynamic-pdb-postgres-1"
DEV_DB="dynamic_pdb_local"
# The local dev database was migrated with these locations (see ../flyway.env), so the
# practice copy keeps them. The production image uses data/prod instead of data/local.
LOCAL_FLYWAY_LOCATIONS="filesystem:/flyway/sql/db/public/structure,filesystem:/flyway/sql/db/public/functions,filesystem:/flyway/sql/db/public/data/common,filesystem:/flyway/sql/db/public/data/local"

# Connection to the copy, set by open_aws_connection or open_local_connection.
DB_HOST=""
DB_PORT=""
DB_NAME=""
DB_USER=""
DB_SSLMODE=""
FLYWAY_LOCATIONS_OVERRIDE=""
TARGET_DESCRIPTION=""
FRESH_COPY_COMMANDS=""
PORT_FORWARD_PID=""
TUNNEL_STARTED=""

# Findings of one run, filled by cmd_run and read by write_report.
FAILURES=""
CHECKS=""

log() { printf '==> %s\n' "$*" >&2; }
die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
usage: rehearsal.sh up | run --branch REF [--target aws|local] | down | status | local-up | local-down
See backend/migrations/rehearsal/README.md.
EOF
}

require_tools() {
	local tool
	for tool in "$@"; do
		command -v "$tool" >/dev/null 2>&1 || die "missing tool: $tool (see README.md, Setup)"
	done
}

# Called as: require_no_args <command> "$@"
require_no_args() {
	[ "$#" -eq 1 ] || die "$1 takes no options"
}

utc_in_hours() {
	date -u -v+"$1"H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d "+$1 hours" +%Y-%m-%dT%H:%M:%SZ
}

add_failure() { FAILURES="$FAILURES- $*"$'\n'; }
add_check() { CHECKS="$CHECKS- $*"$'\n'; }

# --- Kubernetes and AWS ---------------------------------------------------------

kube() { kubectl "${KUBE_ARGS[@]}" "$@"; }

# Prints the decoded value of one key of a secret in $NAMESPACE.
secret_value() {
	local secret="$1" key="$2" encoded
	encoded="$(kube get secret "$secret" -o "jsonpath={.data.$key}")" ||
		die "cannot read secret $NAMESPACE/$secret (do you have access to the $NAMESPACE namespace?)"
	[ -n "$encoded" ] || die "secret $NAMESPACE/$secret has no key $key"
	printf '%s' "$encoded" | base64 --decode
}

# Uses AWS_PROFILE or AWS_ACCESS_KEY_ID when set; otherwise reads the rehearsal
# user's key from the cluster. The key stays in this process's environment only.
load_aws_credentials() {
	if [ -n "${AWS_PROFILE:-}" ] || [ -n "${AWS_ACCESS_KEY_ID:-}" ]; then
		return
	fi
	log "reading AWS credentials from secret $NAMESPACE/$AWS_SECRET_NAME"
	AWS_ACCESS_KEY_ID="$(secret_value "$AWS_SECRET_NAME" AWS_ACCESS_KEY_ID)"
	AWS_SECRET_ACCESS_KEY="$(secret_value "$AWS_SECRET_NAME" AWS_SECRET_ACCESS_KEY)"
	export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY
}

# Prints the instance as JSON, or nothing when it does not exist.
describe_instance() {
	local id="$1" output errors
	errors="$(mktemp)"
	if output="$(aws rds describe-db-instances --db-instance-identifier "$id" --query 'DBInstances[0]' --output json 2>"$errors")"; then
		rm -f "$errors"
		printf '%s' "$output"
		return
	fi
	if grep -q DBInstanceNotFound "$errors"; then
		rm -f "$errors"
		return
	fi
	cat "$errors" >&2
	rm -f "$errors"
	die "cannot describe RDS instance $id"
}

instance_tag() {
	jq -r --arg key "$2" '(.TagList // []) | map(select(.Key == $key)) | .[0].Value // ""' <<<"$1"
}

require_rehearsal_name() {
	local id="$1" protected
	for protected in $PROTECTED_INSTANCES; do
		[ "$id" != "$protected" ] || die "refusing to touch $id: it is a real database"
	done
	[[ "$id" =~ ^dynamic-pdb-rehearsal(-[a-z0-9]+)*$ ]] ||
		die "refusing to touch $id: rehearsal instance names must start with dynamic-pdb-rehearsal"
}

require_rehearsal_tag() {
	local instance="$1"
	[ "$(instance_tag "$instance" purpose)" = "$REHEARSAL_TAG" ] ||
		die "refusing to touch $(jq -r .DBInstanceIdentifier <<<"$instance"): it has no purpose=$REHEARSAL_TAG tag"
}

# Polls until the instance is available with no pending changes.
wait_until_settled() {
	local id="$1" deadline instance status pending
	deadline=$(($(date +%s) + 60 * 60))
	while :; do
		instance="$(describe_instance "$id")"
		[ -n "$instance" ] || die "$id disappeared while waiting"
		status="$(jq -r .DBInstanceStatus <<<"$instance")"
		pending="$(jq -c '.PendingModifiedValues // {}' <<<"$instance")"
		if [ "$status" = available ] && [ "$pending" = "{}" ]; then
			return
		fi
		[ "$(date +%s)" -lt "$deadline" ] ||
			die "$id is still $status after 60 minutes; run the same command again to keep waiting"
		log "  $id: $status"
		sleep 30
	done
}

restore_from_production() {
	local source subnet_group security_groups instance_class storage_type restorable expires
	source="$(describe_instance "$SOURCE_INSTANCE")"
	[ -n "$source" ] || die "production instance $SOURCE_INSTANCE not found in $AWS_REGION"
	subnet_group="$(jq -r .DBSubnetGroup.DBSubnetGroupName <<<"$source")"
	security_groups="$(jq -r '[.VpcSecurityGroups[].VpcSecurityGroupId] | join(" ")' <<<"$source")"
	instance_class="$(jq -r .DBInstanceClass <<<"$source")"
	storage_type="$(jq -r .StorageType <<<"$source")"
	restorable="$(jq -r .LatestRestorableTime <<<"$source")"
	expires="$(utc_in_hours "$EXPIRY_HOURS")"

	log "restoring $SOURCE_INSTANCE as of $restorable into $REHEARSAL_INSTANCE (production is not changed)"
	# shellcheck disable=SC2086 # the security group IDs are separate words on purpose
	aws rds restore-db-instance-to-point-in-time \
		--source-db-instance-identifier "$SOURCE_INSTANCE" \
		--target-db-instance-identifier "$REHEARSAL_INSTANCE" \
		--use-latest-restorable-time \
		--db-subnet-group-name "$subnet_group" \
		--vpc-security-group-ids $security_groups \
		--db-instance-class "$instance_class" \
		--storage-type "$storage_type" \
		--no-publicly-accessible \
		--no-multi-az \
		--no-deletion-protection \
		--tags "Key=purpose,Value=$REHEARSAL_TAG" "Key=expires-at,Value=$expires" \
		"Key=restored-to,Value=$restorable" "Key=created-by,Value=${USER:-unknown}" \
		--query 'DBInstance.DBInstanceIdentifier' --output text >/dev/null
}

# The restore copies production's master password. Give the copy its own, so a leaked
# rehearsal password cannot open production, and keep it in a secret in the cluster.
# The password goes through a 0600 file and stdin, never a command line.
reset_rehearsal_password() {
	local password input
	password="$(openssl rand -hex 24)"
	input="$(mktemp)"
	chmod 600 "$input"
	printf '{"DBInstanceIdentifier":"%s","MasterUserPassword":"%s","ApplyImmediately":true}' \
		"$REHEARSAL_INSTANCE" "$password" >"$input"
	log "giving $REHEARSAL_INSTANCE its own password"
	if ! aws rds modify-db-instance --cli-input-json "file://$input" \
		--query 'DBInstance.DBInstanceIdentifier' --output text >/dev/null; then
		rm -f "$input"
		die "could not set the rehearsal password"
	fi
	rm -f "$input"
	printf 'apiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\n  labels:\n    app.kubernetes.io/part-of: dynamic-pdb-rehearsal\ntype: Opaque\nstringData:\n  password: "%s"\n' \
		"$DB_SECRET_NAME" "$password" | kube apply -f - >/dev/null
	# Give RDS a moment to show the password change as pending before polling.
	sleep 15
}

# --- Commands: AWS copy -----------------------------------------------------------

cmd_up() {
	require_no_args up "$@"
	require_tools aws kubectl jq openssl
	require_rehearsal_name "$REHEARSAL_INSTANCE"
	load_aws_credentials

	local instance
	instance="$(describe_instance "$REHEARSAL_INSTANCE")"
	if [ -z "$instance" ]; then
		restore_from_production
	else
		require_rehearsal_tag "$instance"
		log "$REHEARSAL_INSTANCE already exists ($(jq -r .DBInstanceStatus <<<"$instance")); continuing with it"
	fi
	log "waiting for $REHEARSAL_INSTANCE to become available (a restore usually takes 15-20 minutes)"
	wait_until_settled "$REHEARSAL_INSTANCE"

	if kube get secret "$DB_SECRET_NAME" >/dev/null 2>&1; then
		log "password secret $NAMESPACE/$DB_SECRET_NAME already exists"
	else
		reset_rehearsal_password
		wait_until_settled "$REHEARSAL_INSTANCE"
	fi

	instance="$(describe_instance "$REHEARSAL_INSTANCE")"
	log "ready: $REHEARSAL_INSTANCE, restored to $(instance_tag "$instance" restored-to), expires $(instance_tag "$instance" expires-at)"
	log "next: make rehearsal-run BRANCH=origin/<branch>"
}

cmd_down() {
	require_no_args down "$@"
	require_tools aws kubectl jq
	require_rehearsal_name "$REHEARSAL_INSTANCE"
	load_aws_credentials

	kube delete pod "$TUNNEL_POD" --ignore-not-found >/dev/null
	kube delete secret "$DB_SECRET_NAME" --ignore-not-found >/dev/null
	local instance
	instance="$(describe_instance "$REHEARSAL_INSTANCE")"
	if [ -z "$instance" ]; then
		log "nothing to delete: $REHEARSAL_INSTANCE does not exist"
		return
	fi
	require_rehearsal_tag "$instance"
	log "deleting $REHEARSAL_INSTANCE without a final snapshot (production is not changed)"
	aws rds delete-db-instance --db-instance-identifier "$REHEARSAL_INSTANCE" \
		--skip-final-snapshot --delete-automated-backups \
		--query 'DBInstance.DBInstanceStatus' --output text >/dev/null
	log "waiting for the deletion to finish (usually about 5 minutes)"
	aws rds wait db-instance-deleted --db-instance-identifier "$REHEARSAL_INSTANCE"
	log "deleted $REHEARSAL_INSTANCE"
}

cmd_status() {
	require_no_args status "$@"
	require_tools aws kubectl jq
	load_aws_credentials

	local instances
	instances="$(aws rds describe-db-instances --output json \
		--query "DBInstances[?starts_with(DBInstanceIdentifier, 'dynamic-pdb-rehearsal')]")"
	if [ "$(jq length <<<"$instances")" -eq 0 ]; then
		echo "RDS: no rehearsal instances"
	else
		echo "RDS rehearsal instances (name, status, created, restored to, expires):"
		jq -r '.[] | [.DBInstanceIdentifier, .DBInstanceStatus, (.InstanceCreateTime // "-"),
			(((.TagList // []) | map(select(.Key == "restored-to")) | .[0].Value) // "-"),
			(((.TagList // []) | map(select(.Key == "expires-at")) | .[0].Value) // "-")] | "  " + join("  ")' <<<"$instances"
	fi
	if kube get secret "$DB_SECRET_NAME" >/dev/null 2>&1; then
		echo "password secret: $NAMESPACE/$DB_SECRET_NAME exists"
	else
		echo "password secret: none"
	fi
	if kube get pod "$TUNNEL_POD" >/dev/null 2>&1; then
		echo "tunnel pod: $NAMESPACE/$TUNNEL_POD exists (a run deletes it when it ends)"
	else
		echo "tunnel pod: none"
	fi
}

# --- Commands: local practice copy ---------------------------------------------------

container_running() {
	[ "$(docker inspect -f '{{.State.Running}}' "$1" 2>/dev/null || true)" = true ]
}

cmd_local_up() {
	require_no_args local-up "$@"
	require_tools docker
	container_running "$DEV_CONTAINER" ||
		die "the local dev Postgres ($DEV_CONTAINER) is not running; start it with: make -C backend start-postgres"
	if docker inspect "$LOCAL_CONTAINER" >/dev/null 2>&1; then
		die "$LOCAL_CONTAINER already exists; run make rehearsal-local-down first for a fresh copy"
	fi

	log "starting $LOCAL_IMAGE as $LOCAL_CONTAINER on 127.0.0.1:$LOCAL_PORT"
	docker run --detach --name "$LOCAL_CONTAINER" \
		--label app.kubernetes.io/part-of=dynamic-pdb-rehearsal \
		--env POSTGRES_PASSWORD="$LOCAL_PASSWORD" --env POSTGRES_DB="$LOCAL_DB" \
		--publish "127.0.0.1:$LOCAL_PORT:5432" "$LOCAL_IMAGE" >/dev/null
	# The image starts a temporary server while it initializes, so wait for the init to finish.
	local attempt=0
	until docker logs "$LOCAL_CONTAINER" 2>&1 | grep -q 'PostgreSQL init process complete' &&
		docker exec "$LOCAL_CONTAINER" pg_isready --quiet --username "$LOCAL_USER" --dbname "$LOCAL_DB"; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 60 ] || die "$LOCAL_CONTAINER did not start; see: docker logs $LOCAL_CONTAINER"
		sleep 1
	done

	log "copying $DEV_DB from $DEV_CONTAINER"
	docker exec "$DEV_CONTAINER" pg_dump --username postgres --format custom --no-owner --no-privileges "$DEV_DB" |
		docker exec --interactive "$LOCAL_CONTAINER" pg_restore --username "$LOCAL_USER" --dbname "$LOCAL_DB" \
			--no-owner --no-privileges --exit-on-error
	log "ready: practice copy on Postgres 18.3; next: make rehearsal-run TARGET=local BRANCH=<branch>"
}

cmd_local_down() {
	require_no_args local-down "$@"
	require_tools docker
	if ! docker inspect "$LOCAL_CONTAINER" >/dev/null 2>&1; then
		log "nothing to delete: $LOCAL_CONTAINER does not exist"
		return
	fi
	docker rm --force --volumes "$LOCAL_CONTAINER" >/dev/null
	log "deleted $LOCAL_CONTAINER"
}

# --- Command: run ---------------------------------------------------------------------

cleanup_run() {
	if [ -n "$PORT_FORWARD_PID" ]; then
		kill "$PORT_FORWARD_PID" 2>/dev/null || true
	fi
	if [ -n "$TUNNEL_STARTED" ]; then
		kube delete pod "$TUNNEL_POD" --ignore-not-found --wait=false >/dev/null 2>&1 || true
	fi
}

db_psql() {
	psql "host=$DB_HOST port=$DB_PORT dbname=$DB_NAME user=$DB_USER sslmode=$DB_SSLMODE" \
		--no-psqlrc --set ON_ERROR_STOP=1 --tuples-only --no-align --field-separator '|' "$@"
}

wait_for_database() {
	local attempt=0
	until db_psql --command 'SELECT 1' >/dev/null 2>&1; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 30 ] || die "cannot connect to the copy at $DB_HOST:$DB_PORT"
		sleep 1
	done
}

start_tunnel() {
	local host="$1" port="$2" log_file="$3"
	log "starting tunnel pod $NAMESPACE/$TUNNEL_POD to $host:$port"
	kube delete pod "$TUNNEL_POD" --ignore-not-found >/dev/null
	TUNNEL_STARTED=1
	sed -e "s#__POD_NAME__#$TUNNEL_POD#" -e "s#__NAMESPACE__#$NAMESPACE#" -e "s#__IMAGE__#$TUNNEL_IMAGE#" \
		-e "s#__TARGET_HOST__#$host#" -e "s#__TARGET_PORT__#$port#" "$SCRIPT_DIR/tunnel-pod.yaml" |
		kube apply -f - >/dev/null
	kube wait --for=condition=Ready "pod/$TUNNEL_POD" --timeout=180s >/dev/null ||
		die "the tunnel pod did not become ready; see: kubectl -n $NAMESPACE describe pod $TUNNEL_POD"
	# kubectl directly, not the kube function, so $! is kubectl's own process ID.
	kubectl "${KUBE_ARGS[@]}" port-forward "pod/$TUNNEL_POD" "$TUNNEL_PORT:5432" --address 127.0.0.1 >"$log_file" 2>&1 &
	PORT_FORWARD_PID=$!
}

open_aws_connection() {
	local log_file="$1" instance status rehearsal_host production production_host=""
	load_aws_credentials
	require_rehearsal_name "$REHEARSAL_INSTANCE"
	instance="$(describe_instance "$REHEARSAL_INSTANCE")"
	[ -n "$instance" ] || die "$REHEARSAL_INSTANCE does not exist; run make rehearsal-up first"
	require_rehearsal_tag "$instance"
	status="$(jq -r .DBInstanceStatus <<<"$instance")"
	[ "$status" = available ] || die "$REHEARSAL_INSTANCE is $status; run make rehearsal-up to finish it"
	rehearsal_host="$(jq -r .Endpoint.Address <<<"$instance")"
	production="$(describe_instance "$SOURCE_INSTANCE")"
	if [ -n "$production" ]; then
		production_host="$(jq -r .Endpoint.Address <<<"$production")"
	fi
	[ "$rehearsal_host" != "$production_host" ] || die "the rehearsal endpoint is production's endpoint; refusing"

	DB_NAME="$(jq -r .DBName <<<"$instance")"
	DB_USER="$(jq -r .MasterUsername <<<"$instance")"
	DB_HOST=127.0.0.1
	DB_PORT="$TUNNEL_PORT"
	DB_SSLMODE=require
	PGPASSWORD="$(secret_value "$DB_SECRET_NAME" password)"
	export PGPASSWORD
	TARGET_DESCRIPTION="RDS $REHEARSAL_INSTANCE, PostgreSQL $(jq -r .EngineVersion <<<"$instance"), $(jq -r .DBInstanceClass <<<"$instance"), restored to $(instance_tag "$instance" restored-to)"
	FRESH_COPY_COMMANDS="make rehearsal-down and make rehearsal-up"
	start_tunnel "$rehearsal_host" "$(jq -r .Endpoint.Port <<<"$instance")" "$log_file"
}

open_local_connection() {
	container_running "$LOCAL_CONTAINER" || die "the practice copy is not running; run make rehearsal-local-up first"
	DB_HOST=127.0.0.1
	DB_PORT="$LOCAL_PORT"
	DB_NAME="$LOCAL_DB"
	DB_USER="$LOCAL_USER"
	DB_SSLMODE=disable
	PGPASSWORD="$LOCAL_PASSWORD"
	export PGPASSWORD
	FLYWAY_LOCATIONS_OVERRIDE="$LOCAL_FLYWAY_LOCATIONS"
	TARGET_DESCRIPTION="local practice copy ($LOCAL_IMAGE with a copy of $DEV_DB; timings mean nothing)"
	FRESH_COPY_COMMANDS="make rehearsal-local-down and make rehearsal-local-up"
}

# Builds the migrations image from the branch's own Dockerfile, the way the production
# deploy builds it, with two local changes:
# - The base image normally comes through the cluster's Docker Hub cache
#   (harbor.astera.sh/dockerhub-cache/); here the same image comes straight from Docker Hub.
# - The "# syntax=" line is dropped. It makes Docker fetch a builder image through the
#   credential helper, which can hang for minutes; the Dockerfile uses only basic instructions.
build_migrations_image() {
	local source_dir="$1" image="$2"
	sed -e '/^# *syntax=/d' -e 's#^FROM harbor\.astera\.sh/dockerhub-cache/#FROM #' \
		"$source_dir/Dockerfile" >"$source_dir/Dockerfile.rehearsal"
	docker build --quiet --file "$source_dir/Dockerfile.rehearsal" --tag "$image" "$source_dir" >/dev/null
}

# Runs Flyway from the migrations image. Connection settings reach the container
# through its environment, so the password never appears on a command line.
run_flyway() {
	local image="$1"
	shift
	local location_args=()
	if [ -n "$FLYWAY_LOCATIONS_OVERRIDE" ]; then
		location_args=(--env FLYWAY_LOCATIONS)
	fi
	FLYWAY_URL="jdbc:postgresql://host.docker.internal:$DB_PORT/$DB_NAME?sslmode=$DB_SSLMODE" \
		FLYWAY_USER="$DB_USER" FLYWAY_PASSWORD="$PGPASSWORD" FLYWAY_LOCATIONS="$FLYWAY_LOCATIONS_OVERRIDE" \
		docker run --rm --add-host=host.docker.internal:host-gateway \
		--env FLYWAY_URL --env FLYWAY_USER --env FLYWAY_PASSWORD \
		${location_args[@]+"${location_args[@]}"} \
		"$image" "$@"
}

max_installed_rank() {
	db_psql --command 'SELECT coalesce(max(installed_rank), 0) FROM flyway_schema_history'
}

# Earlier rehearsal runs on this copy are recorded in a schema outside public, which
# Flyway and snapshot.sql do not look at. Prints nothing for a fresh copy.
previous_runs() {
	db_psql --command "SELECT string_agg(ref || ' (' || left(commit, 12) || ')', ', ' ORDER BY ran_at)
		FROM rehearsal_meta.runs" 2>/dev/null || true
}

record_run() {
	db_psql --quiet --set ref="$1" --set commit="$2" >/dev/null <<'SQL'
SET client_min_messages = warning;
CREATE SCHEMA IF NOT EXISTS rehearsal_meta;
CREATE TABLE IF NOT EXISTS rehearsal_meta.runs (ref text NOT NULL, commit text NOT NULL, ran_at timestamptz NOT NULL DEFAULT now());
INSERT INTO rehearsal_meta.runs (ref, commit) VALUES (:'ref', :'commit');
SQL
}

# Runs a SQL file whose output is saved for the report. Returns non-zero without
# stopping the run, so one broken query still leaves a report behind.
save_query() {
	local file="$1" output="$2"
	if ! db_psql --file "$file" >"$output" 2>"$output.err"; then
		log "warning: $(basename "$file") failed; see $output.err"
		return 1
	fi
	rm -f "$output.err"
}

# Compares two snapshot.sql outputs. Prints table|rows before|rows after|change for
# each table that changed. Row counts compare as numbers; checksums compare as text,
# because they are too large for awk's floating-point numbers.
compare_snapshots() {
	awk -F'|' -v OFS='|' '
		NR == FNR { rows[$1] = $2 + 0; sum[$1] = $3 ""; next }
		{
			seen[$1] = 1
			if (!($1 in rows)) { print $1, "-", $2, "new table"; next }
			if ($2 + 0 < rows[$1]) { print $1, rows[$1], $2, "ROWS DECREASED"; next }
			if ($2 + 0 > rows[$1]) { print $1, rows[$1], $2, "rows added"; next }
			if ($3 "" != sum[$1]) { print $1, rows[$1], $2, "values changed" }
		}
		END { for (t in rows) if (!(t in seen)) print t, rows[t], "-", "TABLE GONE" }
	' "$1" "$2" | sort
}

markdown_rows() {
	awk -F'|' '{ line = "|"; for (i = 1; i <= NF; i++) line = line " " $i " |"; print line }' "$1"
}

files_differ() {
	! diff -q "$1" "$2" >/dev/null
}

write_report() {
	local report="$1" dir="$2" branch="$3" commit="$4" verdict="$5" started_at="$6" duration="$7"
	{
		printf '# Migration rehearsal: %s\n\n' "$verdict"
		printf -- '- Branch: %s at %s\n' "$branch" "${commit:0:12}"
		printf -- '- Copy: %s\n' "$TARGET_DESCRIPTION"
		printf -- '- Ran: %s, %s seconds in total\n\n' "$started_at" "$duration"
		if [ -n "$FAILURES" ]; then printf '## Failures\n\n%s\n' "$FAILURES"; fi
		if [ -n "$CHECKS" ]; then printf '## Needs a look\n\n%s\n' "$CHECKS"; fi

		printf '## Migrations applied\n\n'
		if [ -s "$dir/applied.txt" ]; then
			printf '| Version | Description | Time (ms) | Succeeded |\n|---|---|---|---|\n'
			markdown_rows "$dir/applied.txt"
		else
			printf 'None.\n'
		fi

		printf '\n## Tables that changed\n\n'
		if [ ! -f "$dir/table-changes.txt" ]; then
			printf 'Unknown: a snapshot failed.\n'
		elif [ -s "$dir/table-changes.txt" ]; then
			printf '| Table | Rows before | Rows after | Change |\n|---|---|---|---|\n'
			markdown_rows "$dir/table-changes.txt"
			printf '\n"values changed" is expected for tables a data migration rewrites. Compare it with what the PR says it changes.\n'
		else
			printf 'None: every table has the same rows and values as before.\n'
		fi

		printf '\n## Database health after the migrations\n\n'
		if [ -s "$dir/health.txt" ]; then
			printf '| Finding | Detail |\n|---|---|\n'
			markdown_rows "$dir/health.txt"
		else
			printf 'No invalid indexes, failed migrations or NOT VALID constraints.\n'
		fi

		if [ -f "$dir/before-counts.txt" ] && [ -f "$dir/after-counts.txt" ]; then
			printf '\n## Branch check: inventory/counts.sql\n\n'
			if files_differ "$dir/before-counts.txt" "$dir/after-counts.txt"; then
				printf '```diff\n'
				diff "$dir/before-counts.txt" "$dir/after-counts.txt" || true
				printf '```\n'
			else
				printf 'Same before and after.\n'
			fi
		fi
		if [ -s "$dir/inventory-summary.txt" ]; then
			printf '\n## Branch check: inventory/schema_inventory.sql after the migrations\n\n'
			printf '| Check | Records |\n|---|---|\n'
			markdown_rows "$dir/inventory-summary.txt"
		fi
		printf '\nNo other traffic ran during the rehearsal, so locks that wait for running transactions can take longer on production.\n'
	} >"$report"
}

cmd_run() {
	local branch="" target="aws"
	while [ "$#" -gt 0 ]; do
		[ "$#" -ge 2 ] || die "option $1 needs a value"
		case "$1" in
		--branch) branch="$2" ;;
		--target) target="$2" ;;
		*) die "unknown option for run: $1" ;;
		esac
		shift 2
	done
	[ -n "$branch" ] || die "run needs a branch, for example: make rehearsal-run BRANCH=origin/lh/schema-migrations"
	require_tools git docker psql awk
	case "$target" in
	aws) require_tools aws kubectl jq ;;
	local) ;;
	*) die "unknown target: $target (use aws or local)" ;;
	esac

	git -C "$REPO_ROOT" fetch --quiet origin || log "warning: git fetch failed; using the refs you already have"
	local commit
	commit="$(git -C "$REPO_ROOT" rev-parse --verify --quiet "$branch^{commit}")" || die "unknown branch or commit: $branch"

	# The report folder keeps the exact migrations that ran, next to their results.
	local dir source_dir image
	dir="$REPORTS_DIR/$(date -u +%Y%m%dT%H%M%SZ)-$(printf '%s' "$branch" | tr -c 'A-Za-z0-9._-' '-')"
	source_dir="$dir/source"
	mkdir -p "$source_dir"
	git -C "$REPO_ROOT" archive "$commit" backend/migrations | tar -x -C "$source_dir" --strip-components 2
	image="dynamic-pdb-migrations:rehearsal-${commit:0:12}"
	log "building the migrations image for $branch (${commit:0:12})"
	build_migrations_image "$source_dir" "$image"

	trap cleanup_run EXIT
	case "$target" in
	aws) open_aws_connection "$dir/port-forward.log" ;;
	local) open_local_connection ;;
	*) die "unknown target: $target" ;;
	esac
	wait_for_database

	local started_at started earlier counts_sql inventory_sql
	started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	started="$(date +%s)"
	earlier="$(previous_runs)"
	counts_sql="$source_dir/inventory/counts.sql"
	inventory_sql="$source_dir/inventory/schema_inventory.sql"

	log "taking the before snapshot"
	save_query "$SCRIPT_DIR/snapshot.sql" "$dir/before-tables.txt" || die "the before snapshot failed; nothing was migrated"
	if [ -f "$counts_sql" ] && ! save_query "$counts_sql" "$dir/before-counts.txt"; then
		add_check "The branch's inventory/counts.sql failed before the migrations, so it was not compared."
		rm -f "$dir/before-counts.txt"
	fi
	local rank_before rank_after rank_rerun migrate_status=0 rerun_status=0
	rank_before="$(max_installed_rank)"

	log "running the migrations"
	run_flyway "$image" migrate >"$dir/flyway-migrate.log" 2>&1 || migrate_status=$?
	rank_after="$(max_installed_rank)"
	db_psql --command "SELECT coalesce(version, '(repeatable)'), description, execution_time, success
		FROM flyway_schema_history WHERE installed_rank > $rank_before ORDER BY installed_rank" >"$dir/applied.txt"
	rank_rerun="$rank_after"
	if [ "$migrate_status" -eq 0 ]; then
		log "running the migrations a second time (it should do nothing)"
		run_flyway "$image" migrate >"$dir/flyway-rerun.log" 2>&1 || rerun_status=$?
		rank_rerun="$(max_installed_rank)"
	fi

	log "taking the after snapshot"
	if save_query "$SCRIPT_DIR/snapshot.sql" "$dir/after-tables.txt"; then
		compare_snapshots "$dir/before-tables.txt" "$dir/after-tables.txt" >"$dir/table-changes.txt"
	else
		add_failure "The after snapshot failed, so lost rows could not be checked."
	fi
	if [ -f "$dir/before-counts.txt" ] && ! save_query "$counts_sql" "$dir/after-counts.txt"; then
		add_check "The branch's inventory/counts.sql failed after the migrations."
		rm -f "$dir/after-counts.txt"
	fi
	save_query "$SCRIPT_DIR/health.sql" "$dir/health.txt" || add_failure "The health check query failed."
	if [ -f "$inventory_sql" ] && [ "$migrate_status" -eq 0 ]; then
		if save_query "$inventory_sql" "$dir/inventory.txt"; then
			awk -F'|' '{ count[$1]++ } END { for (c in count) print c "|" count[c] }' "$dir/inventory.txt" |
				sort >"$dir/inventory-summary.txt"
		else
			add_check "The branch's inventory/schema_inventory.sql failed after the migrations."
		fi
	fi
	record_run "$branch" "$commit"

	# FAIL means the migrations are broken; CHECK means a result a person has to judge.
	if [ "$migrate_status" -ne 0 ]; then
		add_failure "Flyway migrate exited with $migrate_status; see flyway-migrate.log."
	fi
	if awk -F'|' '$4 == "f" { found = 1 } END { exit !found }' "$dir/applied.txt"; then
		add_failure "A migration is recorded as failed in flyway_schema_history."
	fi
	if grep -q '^invalid_index|' "$dir/health.txt" 2>/dev/null; then
		add_failure "An index was left invalid (a CREATE INDEX CONCURRENTLY failed)."
	fi
	if grep -q '^failed_migration|' "$dir/health.txt" 2>/dev/null; then
		add_failure "flyway_schema_history has failed rows."
	fi
	if [ "$migrate_status" -eq 0 ] && { [ "$rerun_status" -ne 0 ] || [ "$rank_rerun" != "$rank_after" ]; }; then
		add_failure "Running the migrations a second time did something; see flyway-rerun.log."
	fi
	if grep -q -e '|ROWS DECREASED$' -e '|TABLE GONE$' "$dir/table-changes.txt" 2>/dev/null; then
		add_check "Rows disappeared or a table is gone. Make sure the branch means to do that."
	fi
	if [ -f "$dir/after-counts.txt" ] && files_differ "$dir/before-counts.txt" "$dir/after-counts.txt"; then
		add_check "The branch's inventory/counts.sql changed; it lists numbers the migrations must keep."
	fi
	if [ "$migrate_status" -eq 0 ] && [ "$rank_after" = "$rank_before" ]; then
		add_check "No migrations were pending. The copy may already have this branch's migrations."
	fi
	if [ -n "$earlier" ]; then
		add_check "This copy was already used for: $earlier. It no longer matches production; run $FRESH_COPY_COMMANDS for a fresh one."
	fi

	local verdict
	if [ -n "$FAILURES" ]; then
		verdict=FAIL
	elif [ -n "$CHECKS" ]; then
		verdict=CHECK
	else
		verdict=PASS
	fi
	write_report "$dir/report.md" "$dir" "$branch" "$commit" "$verdict" "$started_at" "$(($(date +%s) - started))"
	log "verdict: $verdict"
	log "report: $dir/report.md"
	[ "$verdict" != FAIL ]
}

main() {
	local command="${1:-}"
	if [ "$#" -gt 0 ]; then shift; fi
	case "$command" in
	up) cmd_up "$@" ;;
	run) cmd_run "$@" ;;
	down) cmd_down "$@" ;;
	status) cmd_status "$@" ;;
	local-up) cmd_local_up "$@" ;;
	local-down) cmd_local_down "$@" ;;
	help | -h | --help) usage ;;
	"")
		usage >&2
		exit 1
		;;
	*)
		usage >&2
		die "unknown command: $command"
		;;
	esac
}

main "$@"
