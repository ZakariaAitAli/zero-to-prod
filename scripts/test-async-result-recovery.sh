#!/usr/bin/env bash
# Run against an isolated PostgreSQL Compose project with initialized roles.
set -euo pipefail
root_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
project_name="${ZTP_COMPOSE_PROJECT_NAME:-zero-to-prod-local}"
admin_user="${ZTP_POSTGRES_ADMIN_USER:-zero_to_prod_admin}"
migrator_user="${ZTP_POSTGRES_MIGRATOR_USER:-zero_to_prod_migrator}"
migrate_image="${MIGRATE_IMAGE:-zero-to-prod-migrate:${MIGRATE_VERSION:-v4.20.1}}"
legacy_db=issue117_legacy_test
restore_db=issue117_restore_test
# Build the selected local image if absent; do not try pulling a local image name.
if ! docker image inspect "$migrate_image" >/dev/null 2>&1; then
 MIGRATE_IMAGE="$migrate_image" "$root_dir/tools/postgres-local" build-migrate
fi
artifact_dir="$(mktemp -d)"
created_legacy=false
created_restore=false
compose() { docker compose --project-name "$project_name" -f "$root_dir/infra/local/compose.yaml" "$@"; }
admin() { compose exec -T postgres psql -U "$admin_user" -d postgres -v ON_ERROR_STOP=1 "$@"; }
sql() { compose exec -T -e "PGPASSWORD=${ZTP_POSTGRES_MIGRATOR_PASSWORD:-zero-to-prod-local-migrator}" postgres psql -h 127.0.0.1 -U "$migrator_user" -d "$1" -v ON_ERROR_STOP=1 "${@:2}"; }
migrate_legacy() {
 docker run --rm --network "${project_name}_default" \
   -v "$root_dir/apps/work-items/migrations:/migrations:ro" \
   "$migrate_image" \
   -path=/migrations \
   -database "postgres://${migrator_user}:${ZTP_POSTGRES_MIGRATOR_PASSWORD:-zero-to-prod-local-migrator}@postgres:5432/${legacy_db}?sslmode=disable" "$@"
}
cleanup() {
 local original_status=$?
 local cleanup_status=0
 if $created_legacy; then admin -c "DROP DATABASE $legacy_db" >/dev/null || cleanup_status=1; fi
 if $created_restore; then admin -c "DROP DATABASE $restore_db" >/dev/null || cleanup_status=1; fi
 rm -rf -- "$artifact_dir" || cleanup_status=1
 if [[ "$original_status" != 0 ]]; then return "$original_status"; fi
 return "$cleanup_status"
}
trap cleanup EXIT
admin -c "CREATE DATABASE $legacy_db OWNER $migrator_user" >/dev/null
created_legacy=true
admin -c "CREATE DATABASE $restore_db OWNER $migrator_user" >/dev/null
created_restore=true
for database in "$legacy_db" "$restore_db"; do
 compose exec -T postgres psql -U "$admin_user" -d "$database" -v ON_ERROR_STOP=1 -c 'GRANT USAGE ON SCHEMA public TO zero_to_prod_app, zero_to_prod_worker' >/dev/null
 if [[ "$database" == "$legacy_db" ]]; then
   migrate_legacy goto 4
 else
   for migration in "$root_dir"/apps/work-items/migrations/00000{1,2,3,4}_*.up.sql; do sql "$database" -1 < "$migration" >/dev/null; done
 fi
done
sql "$legacy_db" -c "INSERT INTO public.work_items(title,status) VALUES ('legacy completion','done')" >/dev/null
if migrate_legacy up >"$artifact_dir/migration.log" 2>&1; then
 echo 'error: populated legacy migration succeeded' >&2; exit 1
fi
grep -q 'requires an empty application database' "$artifact_dir/migration.log"
test "$(sql "$legacy_db" -Atc 'SELECT version,dirty FROM public.schema_migrations')" = '5|t'
migrate_legacy force 4
test "$(sql "$legacy_db" -Atc 'SELECT version,dirty FROM public.schema_migrations')" = '4|f'
test "$(sql "$legacy_db" -Atc "SELECT count(*), to_regclass('public.work_item_results') FROM public.work_items")" = '1|'
# A legacy archive is rejected before restoring any data.
compose exec -T -e "PGPASSWORD=${ZTP_POSTGRES_MIGRATOR_PASSWORD:-zero-to-prod-local-migrator}" postgres pg_dump -h 127.0.0.1 -U "$migrator_user" -d "$legacy_db" -Fc --data-only --table=public.work_items --table=public.processing_jobs --table=public.outbox_messages > "$artifact_dir/legacy.dump"
if "$root_dir/tools/postgres-backup-local" validate "$artifact_dir/legacy.dump" >"$artifact_dir/legacy.log" 2>&1; then echo 'error: legacy backup accepted' >&2; exit 1; fi
grep -q 'incompatible pre-#117 backup' "$artifact_dir/legacy.log"
sql "$legacy_db" -c 'DELETE FROM public.work_items' >/dev/null
migrate_legacy up
sql "$legacy_db" -1 <<'SQL' >/dev/null
INSERT INTO public.work_items(title) VALUES ('Review API design');
INSERT INTO public.processing_jobs(work_item_id) SELECT id FROM public.work_items;
INSERT INTO public.work_item_results(work_item_id,processing_job_id,input_title,analysis_version,character_count,word_count) SELECT work_item_id,id,'Review API design',1,17,3 FROM public.processing_jobs;
UPDATE public.work_items SET status='done';
UPDATE public.processing_jobs SET state='succeeded',attempt_count=1,finished_at=now();
INSERT INTO public.outbox_messages(processing_job_id,event_type,payload,published_at) SELECT id,'work_item.process','{}',now() FROM public.processing_jobs;
SQL
sql "$restore_db" -1 < "$root_dir/apps/work-items/migrations/000005_add_title_analysis.up.sql" >/dev/null
ZTP_POSTGRES_DB="$legacy_db" "$root_dir/tools/postgres-backup-local" create "$artifact_dir/current.dump"
"$root_dir/tools/postgres-backup-local" validate "$artifact_dir/current.dump"
ZTP_POSTGRES_DB="$restore_db" "$root_dir/tools/postgres-backup-local" restore "$artifact_dir/current.dump"
source_state="$(sql "$legacy_db" -Atc "SELECT json_build_object('items',(SELECT json_agg(i ORDER BY id) FROM public.work_items i),'jobs',(SELECT json_agg(j ORDER BY id) FROM public.processing_jobs j),'outbox',(SELECT json_agg(o ORDER BY id) FROM public.outbox_messages o),'results',(SELECT json_agg(r ORDER BY work_item_id) FROM public.work_item_results r),'sequences',(SELECT json_agg(s ORDER BY sequencename) FROM pg_sequences s WHERE schemaname='public'))")"
restored_state="$(sql "$restore_db" -Atc "SELECT json_build_object('items',(SELECT json_agg(i ORDER BY id) FROM public.work_items i),'jobs',(SELECT json_agg(j ORDER BY id) FROM public.processing_jobs j),'outbox',(SELECT json_agg(o ORDER BY id) FROM public.outbox_messages o),'results',(SELECT json_agg(r ORDER BY work_item_id) FROM public.work_item_results r),'sequences',(SELECT json_agg(s ORDER BY sequencename) FROM pg_sequences s WHERE schemaname='public'))")"
test "$source_state" = "$restored_state"
echo 'Legacy migration refusal, dirty-metadata recovery, backup compatibility, and result restore passed.'
