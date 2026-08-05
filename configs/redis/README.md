# configs/redis

`redis.conf` in this directory is **generated on the deploy host and is not in git**
(it is listed in `.gitignore`). It contains the Redis password.

## Why the config file exists

Redis used to be started with the password on its command line:

```yaml
command: redis-server --appendonly yes ... --requirepass ${REDIS_PASSWORD}
healthcheck:
  test: ["CMD", "redis-cli", "-a", "${REDIS_PASSWORD}", "ping"]
```

Both forms publish the secret to anyone holding the Docker socket:

| Form | Where the secret ended up |
|---|---|
| `--requirepass` on the argv | the container's `Config.Cmd`, i.e. plain `docker inspect sentinel-redis` |
| `redis-cli -a <pw>` healthcheck | the **Docker event stream** — the `exec_create` / `exec_start` `Action` strings, re-emitted every `interval` (10s), forever, to every consumer of `docker events` |

The healthcheck path was the serious one: it was a continuous broadcast, and
`infra-traefik` consumes the event stream on this host permanently.

Moving `requirepass` into this file makes the recorded argv
`redis-server /usr/local/etc/redis/redis.conf`, and the healthcheck reads the
secret at runtime from the same file, so the event stream only ever shows the
literal, unexpanded shell text.

## Generating it

```bash
bash scripts/generate-redis-conf.sh          # renders configs/redis/redis.conf from .env
docker compose up -d --force-recreate redis  # the running container does not reload it
```

`--force-recreate` is required: the `redis` service no longer interpolates
`REDIS_PASSWORD`, so its compose config hash does not change when `.env` changes
and a plain `docker compose up -d` would leave the old container running.

To rotate the password itself (mints a new secret, updates `.env`, regenerates
this file, recreates both `redis` and `backend`, verifies, and rolls back on any
failure):

```bash
bash scripts/rotate-redis-password.sh
bash scripts/rotate-redis-password.sh --verify   # re-run the checks only
```

## Permissions

`0440 root:<redis gid>` — deliberately **not** `0400 root:root`. The official
redis image's entrypoint drops privileges to the `redis` user before exec'ing
`redis-server`, so a root-only-readable config makes the server fail to start.
The healthcheck can still read it because the image sets no `USER`, so
`docker exec` runs as root. The generator resolves the gid from the image at
render time rather than hardcoding it, so an image bump that changes the gid
fails loudly instead of producing a crash loop.

## First deploy on a new host

Generate the file **before** the first `docker compose up`. If it does not
exist, Docker creates a *directory* at the bind-mount path and Redis fails to
start.
