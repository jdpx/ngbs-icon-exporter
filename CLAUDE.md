# CLAUDE.md — working in the `ngbs-icon-exporter` repo

A Prometheus exporter for **NGBS iCON** underfloor heating/cooling controllers
([ngbs.hu](https://ngbs.hu)). Public repo: `github.com/jdpx/ngbs-icon-exporter`.

The `README.md` is the user-facing reference — full metric list, flags, env vars
and scrape config live there. Don't duplicate it here; update it when behaviour
changes.

## Layout

| Path | What it is |
| --- | --- |
| `main.go` | Flag/env parsing, HTTP server, wiring |
| `ngbs/client.go` | Talks to the controller (login + `datapoll`), maps JSON → structs |
| `collector/collector.go` | `prometheus.Collector`, one controller scrape per Prometheus scrape |
| `ngbs/testdata/` | Captured real `datapoll` JSON — the fixtures the tests run against |
| `grafana/dashboard.json` | Importable dashboard (declares a `DS_PROMETHEUS` input) |

## Critical rules

1. **Read-only, always.** The exporter must never write a setting to the
   controller. It speaks exactly two POSTs to `/index.php` (`form=login`, then
   `tab=datapoll`) and nothing else. Do not add a request that mutates state —
   this is a real heating system in an occupied flat.
2. **Never invent metric semantics.** The controller API is undocumented and
   reverse-engineered. If you're unsure what a `datapoll` field means, say so
   and leave it unexported rather than guessing at a name or unit.
3. **Metric names are a public contract.** Renaming or changing the unit of an
   existing `ngbs_icon_*` metric breaks the committed dashboard and the live
   Grafana dashboard (uid `ngbs-icon-budapest`). Update `grafana/dashboard.json`
   and the README table in the same change.
4. **Booleans are `0`/`1` gauges**, temperatures are Celsius, durations are
   seconds — follow Prometheus base-unit convention for anything new.
5. **Don't commit real credentials or a real SysID.** Test fixtures should keep
   placeholder room names and a fake SysID.

## Build / test

```bash
go vet ./...
go test -race ./...
go build ./...
```

That's exactly what CI runs (`.github/workflows/ci.yml`, Go 1.24) — run all
three before pushing. Prefer extending the `ngbs/testdata` fixtures over adding
a live-controller test; there is no controller in CI.

## Releasing

Unlike the other jdpx exporters, **this repo does not auto-release on push to
`main`.** `.github/workflows/release.yml` fires on a `v*` tag only, then
GoReleaser publishes multi-arch binaries and a GHCR image. So tagging is a
deliberate act — push to `main` freely.

## Deployment

Deployed by the homelab repo, not from here: `~/code/jdpx-local`, role
`ansible/roles/ngbs_icon`, wired into `ansible/pi.yml` and gated behind
`ngbs_icon_enabled` in host_vars (Budapest only — that's where the controller
is). Metrics reach Prometheus via Alloy push. After releasing a new version,
bump the pinned image in the homelab repo's host_vars and deploy there.
