#!/bin/sh
set -eu

backup_root=${BACKUP_ROOT:-/var/backups/sub2api}
retention_days=${BACKUP_RETENTION_DAYS:-14}
stamp=$(date -u +%Y%m%dT%H%M%SZ)
work_dir="$backup_root/$stamp"

install -d -m 700 "$work_dir"

# Custom-format dumps are compressed and support selective restore.
docker exec sub2api-postgres pg_dump \
  -U sub2api \
  -d sub2api \
  --format=custom \
  --compress=6 > "$work_dir/postgres.dump"

docker exec sub2api-redis sh -ec '
  if [ -z "${REDISCLI_AUTH:-}" ]; then
    unset REDISCLI_AUTH
  fi
  redis-cli --no-auth-warning SAVE >/dev/null
  cat /data/dump.rdb
' > "$work_dir/redis.rdb"

docker cp sub2api:/app/data/. "$work_dir/sub2api-data"
cp -a /opt/sub2api/deploy/docker-compose.yml "$work_dir/"
cp -a /opt/sub2api/deploy/migration/docker-compose.target.yml "$work_dir/"
cp -a /opt/sub2api/deploy/.env "$work_dir/deploy.env"
cp -a /opt/sub2api/deploy/migration/nginx "$work_dir/"
cp -a /opt/sub2api/.dev/sing-box/config.json "$work_dir/sing-box-config.json"
cp -a /opt/sub2api/.dev/vless-relay/sub "$work_dir/vless-relay-sub"
chmod -R go-rwx "$work_dir"

find "$backup_root" -mindepth 1 -maxdepth 1 -type d \
  -mtime "+$retention_days" -exec rm -rf -- {} +
