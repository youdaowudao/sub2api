#!/usr/bin/env bash
# Isolated CI smoke of the exact single-platform image, no host ports or production data.
set -Eeuo pipefail
umask 077
image=${1:?missing image}
version=${2:?missing version}
owner=$(python3 -c 'import uuid; print(uuid.uuid4().hex)')
prefix="s2a-soak-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}-${owner:0:8}"
network="${prefix}-network"
label="org.sub2api.deploy-smoke=$owner"
cleanup() {
  local rc=0 container_ids network_ids id
  if container_ids=$(docker container ls -aq --filter "label=$label"); then
    for id in $container_ids; do docker container rm -f -v "$id" >/dev/null || rc=1; done
  else rc=1; fi
  if network_ids=$(docker network ls -q --filter "label=$label"); then
    for id in $network_ids; do docker network rm "$id" >/dev/null || rc=1; done
  else rc=1; fi
  return "$rc"
}
trap 'rc=$?; trap - EXIT; cleanup || rc=1; exit "$rc"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Pull only the test dependencies, before joining the isolated network.
docker pull postgres:18-alpine
docker pull redis:8-alpine
docker network create --internal --label "$label" "$network" >/dev/null
pw=$(python3 -c 'import secrets; print(secrets.token_hex(12))')
docker run -d --pull never --label "$label" --name "$prefix-pg" --network "$network" --network-alias pg \
  -e POSTGRES_USER=sub2api -e POSTGRES_PASSWORD="$pw" -e POSTGRES_DB=sub2api postgres:18-alpine >/dev/null
docker run -d --pull never --label "$label" --name "$prefix-redis" --network "$network" --network-alias redis redis:8-alpine >/dev/null
for i in $(seq 60); do
  if docker exec "$prefix-pg" pg_isready -q -h 127.0.0.1 -U sub2api -d sub2api && \
     [[ $(docker exec "$prefix-redis" redis-cli ping) = PONG ]]; then break; fi
  [[ $i -lt 60 ]] || { echo '试运行依赖未就绪' >&2; exit 1; }
  sleep 2
done
docker run -d --pull never --label "$label" --name "$prefix-app" --network "$network" \
  -e AUTO_SETUP=true -e SERVER_HOST=0.0.0.0 -e SERVER_PORT=8080 -e TZ=Asia/Shanghai \
  -e DATABASE_HOST=pg -e DATABASE_PORT=5432 -e DATABASE_USER=sub2api \
  -e DATABASE_PASSWORD="$pw" -e DATABASE_DBNAME=sub2api -e DATABASE_SSLMODE=disable \
  -e REDIS_HOST=redis -e REDIS_PORT=6379 -e ADMIN_EMAIL=admin@example.com -e ADMIN_PASSWORD="$pw" "$image" >/dev/null
for i in $(seq 48); do
  st=$(docker inspect -f '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' "$prefix-app")
  [[ $st = 'running healthy' ]] && break
  if [[ ${st%% *} != running || ${st#* } = unhealthy || $i -eq 48 ]]; then
    # Startup logs may contain test credentials; do not print them.
    printf '应用试运行失败：%s\n' "$st" >&2; exit 1
  fi
  sleep 5
done
docker exec "$prefix-app" wget -qO- http://127.0.0.1:8080/api/v1/settings/public | \
  python3 -c 'import json,sys; s=json.load(sys.stdin); assert s.get("code")==0 and s["data"].get("version")==sys.argv[1], "试运行接口版本不符"' "$version"
# Cleanup must succeed before the caller can publish.
cleanup
trap - EXIT INT TERM
printf 'PASS 隔离 PG18、Redis8、应用健康和接口版本；容器与网络已回收\n'
