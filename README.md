# ngbs-icon-exporter

A [Prometheus](https://prometheus.io) exporter for **NGBS iCON** underfloor /
in-wall heating and cooling controllers ([ngbs.hu](https://ngbs.hu)).

The iCON controller ships a local web interface but no app, no cloud, and no
documented API — so there's no easy way to see what your system is actually
doing over time. This exporter logs in to that web interface and exposes every
value it reports (per-room temperature, humidity, dew point, setpoints,
pump/valve/actuator state, alarms and system mode) as Prometheus metrics, ready
to graph in Grafana.

> Unofficial project. Not affiliated with or endorsed by NGBS. It only reads
> data — it never changes any setting on your controller.

## Metrics

All metrics are prefixed `ngbs_icon_`. Boolean flags are exposed as `0`/`1`
gauges. Per-room metrics carry `id` (e.g. `1.1`) and `name` (the room name you
set on the thermostat) labels.

### System

| Metric | Description |
| --- | --- |
| `ngbs_icon_up` | `1` if the last controller scrape succeeded |
| `ngbs_icon_scrape_duration_seconds` | Duration of the controller scrape |
| `ngbs_icon_info{sysid,version,firmware,mac}` | Constant `1`, controller metadata |
| `ngbs_icon_system_on` | System powered on |
| `ngbs_icon_cooling_mode` | System H/C mode: `1` = cooling, `0` = heating |
| `ngbs_icon_eco_mode` | System C/E mode: `1` = economy, `0` = comfort |
| `ngbs_icon_water_temperature_celsius` | Water / flow temperature |
| `ngbs_icon_external_temperature_celsius` | External / outdoor temperature |
| `ngbs_icon_pump_active` | Circulation pump running |
| `ngbs_icon_error` / `ngbs_icon_overheat` / `ngbs_icon_water_frost` | Fault flags |
| `ngbs_icon_setpoint_{heating,cooling}_celsius` | System setpoints |
| `ngbs_icon_setpoint_eco_{heating,cooling}_celsius` | System eco setpoints |
| `ngbs_icon_uptime_seconds` | Controller uptime |

### Per room

| Metric | Description |
| --- | --- |
| `ngbs_icon_room_temperature_celsius` | Measured temperature |
| `ngbs_icon_room_humidity_percent` | Relative humidity |
| `ngbs_icon_room_dew_point_celsius` | Calculated dew point |
| `ngbs_icon_room_setpoint_{heating,cooling}_celsius` | Room setpoints |
| `ngbs_icon_room_setpoint_eco_{heating,cooling}_celsius` | Room eco setpoints |
| `ngbs_icon_room_limit_celsius` | Manual +/- adjustment range |
| `ngbs_icon_room_installed` / `ngbs_icon_room_live` | Channel installed / sensor reporting |
| `ngbs_icon_room_cooling_mode` / `ngbs_icon_room_eco_mode` | Per-room H/C and C/E mode |
| `ngbs_icon_room_output_active` | Actuator output active (room calling for heat/cool) |
| `ngbs_icon_room_water_pump` / `ngbs_icon_room_mixing_valve` | Pump / valve demand |
| `ngbs_icon_room_lock` | Thermostat locked |
| `ngbs_icon_room_condensation` | Condensation / dew-point warning |
| `ngbs_icon_room_digital_input` / `ngbs_icon_room_frost` | Digital input / frost alarm |
| `ngbs_icon_room_reg_b_{heating,cooling}` | Reg-B heating / cooling enabled |

By default, thermostat channels that are not installed are omitted. Pass
`--include-inactive` to export them too.

## Usage

The exporter needs your controller's address and **SysID** (printed on the
controller and shown on the login page). The password defaults to the SysID,
matching the controller's factory default.

### Binary

Download a release from the [releases page](https://github.com/jdpx/ngbs-icon-exporter/releases), then:

```bash
ngbs-icon-exporter \
  --controller http://192.168.1.50 \
  --sysid 500xxxxxxxxx
# metrics now on http://localhost:9924/metrics
```

### Docker

```bash
docker run -p 9924:9924 \
  -e NGBS_CONTROLLER=http://192.168.1.50 \
  -e NGBS_SYSID=500xxxxxxxxx \
  ghcr.io/jdpx/ngbs-icon-exporter:latest
```

### Configuration

Every flag has an environment-variable equivalent.

| Flag | Env | Default | Description |
| --- | --- | --- | --- |
| `--controller` | `NGBS_CONTROLLER` | `http://192.168.0.1` | Base URL of the controller |
| `--sysid` | `NGBS_SYSID` | *(required)* | Controller SysID / login |
| `--password` | `NGBS_PASSWORD` | *(SysID)* | Login password |
| `--web.listen-address` | `NGBS_LISTEN` | `:9924` | Metrics listen address |
| `--web.telemetry-path` | | `/metrics` | Metrics path |
| `--timeout` | | `10s` | Per-scrape controller timeout |
| `--include-inactive` | | `false` | Export uninstalled channels too |

### Prometheus scrape config

The exporter scrapes the controller once per Prometheus scrape, so your scrape
interval is your poll interval. A heating/cooling system moves slowly — 60s is
plenty:

```yaml
scrape_configs:
  - job_name: ngbs_icon
    scrape_interval: 60s
    static_configs:
      - targets: ["localhost:9924"]
```

## How it works

The controller's web UI is a PHP app. Its JavaScript authenticates with a form
POST and then polls a single endpoint for the whole system state:

```
POST /index.php   sysid=<id>&password=<pw>&lang=en&tab=login&form=login
POST /index.php   tab=datapoll   ->  JSON with system + per-thermostat state
```

This exporter speaks exactly those two requests and maps the returned JSON onto
the metrics above. It reuses the session cookie and transparently re-logs-in
when the session expires.

## Building

```bash
go build ./...
go test ./...
```

## Licence

[MIT](LICENSE)
