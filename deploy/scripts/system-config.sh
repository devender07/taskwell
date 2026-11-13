#!/bin/sh
# Puts the keys of deploy/system.json into the system_config table, in one transaction. An unknown key or a
# value that isn't a string or number fails the whole run. The services pick the change up at once (the same
# notification the admin Config page sends).
# Usage, from anywhere in the repo: deploy/scripts/system-config.sh [user@host | local]   (default devender@ftp.fotospace.co.in)
#   FILE=path/to/other.json deploy/scripts/system-config.sh local
set -eu

HERE=$(cd "$(dirname "$0")" && pwd)
FILE=${FILE:-$HERE/../system.json}
TARGET=${1:-devender@ftp.fotospace.co.in}
[ -f "$FILE" ] || { echo "no $FILE, copy deploy/system.json.example to it and fill it in"; exit 1; }

# The file goes in dollar-quoted with a random tag, so nothing in it needs escaping; Postgres parses the JSON.
TAG=cfg$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')
SQL="begin;
create temp table cfg on commit drop as select \$$TAG\$$(cat "$FILE")\$$TAG\$::jsonb as j;
do \$\$ declare bad text; begin
  if jsonb_typeof((select j from cfg)) <> 'object' then raise exception 'the file must be one JSON object'; end if;
  select string_agg(key, ', ') into bad from cfg, jsonb_each(j)
    where key not in (select key from system_config) or jsonb_typeof(value) not in ('string', 'number');
  if bad is not null then raise exception 'unknown keys or values not a string or number: %', bad; end if;
  perform pg_notify('realtime', '{\"aud\":\"admin\",\"event\":{\"type\":\"event\",\"kind\":\"admin.config\",\"paths\":[\"/config\"]}}');
end \$\$;
update system_config s set value = v.value, updated_at = now() from cfg, jsonb_each_text(j) v
  where s.key = v.key and s.value is distinct from v.value
  returning 'updated ' || s.key;
commit;"

PSQL="docker exec -i taskwell-postgres psql -U taskwell -d taskwell -v ON_ERROR_STOP=1 -qAt"
if [ "$TARGET" = local ]; then
  printf '%s\n' "$SQL" | $PSQL
else
  printf '%s\n' "$SQL" | ssh "$TARGET" "$PSQL"
fi
echo "system_config up to date"
