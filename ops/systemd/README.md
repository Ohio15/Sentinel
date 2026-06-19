# NEXUS host ops — systemd units

Host-level units that run on the NEXUS Docker host (not inside a container).

## ensure-edge-network

Works around a Docker daemon race in which an external-network endpoint is not
always restored on daemon restart. On the 2026-06-18 reboot, `sentinel-backend`
came up attached only to `sentinel_sentinel-network` and not `edge`, so
`infra-traefik` (which routes by container DNS name on `edge`) got NXDOMAIN and
returned **502 for every `/api` route** — sentinelrmm.us API was down ~22h until
the container was manually reconnected. This oneshot deterministically
reattaches the affected containers to `edge` on every boot.

### Install (on NEXUS)

```bash
sudo cp ops/systemd/ensure-edge-network.sh /usr/local/bin/ensure-edge-network.sh
sudo chmod 755 /usr/local/bin/ensure-edge-network.sh
sudo cp ops/systemd/ensure-edge-network.service /etc/systemd/system/ensure-edge-network.service
sudo systemctl daemon-reload
sudo systemctl enable --now ensure-edge-network.service
```

### Verify

```bash
systemctl status ensure-edge-network.service
journalctl -u ensure-edge-network.service        # expect "OK <name> already on edge" lines
```

It is idempotent — running it when everything is already attached is a no-op.

> Note: this guards the **boot** case. A deploy that recreates a container uses
> `docker compose`, which attaches `edge` from the compose definition, so the
> deploy path is already covered.
