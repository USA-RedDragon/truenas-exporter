# truenas-exporter

A Prometheus exporter for TrueNAS SCALE 25.04 and later. It keeps one logged-in connection to the TrueNAS JSON-RPC websocket API, polls it on a timer and serves the last result on `/metrics`, so scrapes never hit TrueNAS directly. `/healthz` returns 200 while the HTTP server runs.

## Metrics

| Metric | Labels | Description |
|---|---|---|
| `truenas_pool_info` | pool, status, status_code | Pool status, always 1 |
| `truenas_pool_online`, `truenas_pool_healthy`, `truenas_pool_warning` | pool | Pool state flags |
| `truenas_pool_size_bytes`, `truenas_pool_allocated_bytes`, `truenas_pool_free_bytes` | pool | Capacity |
| `truenas_pool_scan_active`, `truenas_pool_scan_progress_ratio`, `truenas_pool_scan_errors`, `truenas_pool_scan_start_timestamp_seconds`, `truenas_pool_scan_end_timestamp_seconds`, `truenas_pool_scan_seconds_left` | pool, function | Running or last scrub or resilver. ZFS keeps only the latest scan |
| `truenas_pool_vdev_online`, `truenas_pool_vdev_status` | pool, class, vdev, name, type, disk | vdev and device state |
| `truenas_pool_vdev_read_errors`, `truenas_pool_vdev_write_errors`, `truenas_pool_vdev_checksum_errors` | pool, class, vdev, name, type, disk | ZFS error counters since import or `zpool clear` |
| `truenas_disk_info` | disk, serial, model, type | Disk identity |
| `truenas_disk_temperature_celsius` | disk | Disk temperature |
| `truenas_disk_smart_last_test_info`, `truenas_disk_smart_last_test_failed`, `truenas_disk_smart_last_test_power_on_hours_ago` | disk, test, status | Most recent SMART self-test |
| `truenas_alerts` | klass, level, dismissed | Active TrueNAS alerts |
| `truenas_arc_size_bytes` | | ZFS ARC size |
| `truenas_collector_success`, `truenas_collector_duration_seconds`, `truenas_collector_last_success_timestamp_seconds` | collector | Health of each fetch |

On TrueNAS 25.04, `smart.test.results` fails for API keys without `DISK_WRITE` or full admin: its legacy return schema rejects newer parser fields such as `power_on_hours_ago` and the NVMe-only fields. TrueNAS 25.10 removes the SMART API from the middleware. Set `poll.smart-interval` to `0` on both; TrueNAS still raises a `SMART` alert, which shows up in `truenas_alerts`.

## TrueNAS access

Create a user with no password or shell, a privilege for its group with the `POOL_READ`, `DISK_READ`, `REPORTING_READ` and `ALERT_LIST_READ` roles, and an API key for the user. Use a `wss://` URL: TrueNAS revokes API keys that are sent over plain `ws://`. The default TrueNAS certificate is self-signed, so pin it with `truenas.tls-fingerprint`:

```sh
openssl s_client -connect truenas.local:443 </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256
```

## Configuration

Configuration is read from `config.yaml` in the working directory, environment variables, and command-line flags. Copy [config.example.yaml](config.example.yaml) to start.

<!-- configulator:begin -->

| Key                            | Type    | Default | Environment                    | Flag                             | Description                                                                                                                                                                                                 |
|--------------------------------|---------|---------|--------------------------------|----------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `log-level`                    | string  | `info`  | `LOG_LEVEL`                    | `--log-level`                    | Logging level for the application. One of debug, info, warn, or error                                                                                                                                       |
| `http.bind`                    | string  | `[::]`  | `HTTP_BIND`                    | `--http.bind`                    | Address to listen on. The default, [::], listens on all interfaces                                                                                                                                          |
| `http.port`                    | integer | `9814`  | `HTTP_PORT`                    | `--http.port`                    | Port to serve /metrics and /healthz on                                                                                                                                                                      |
| `truenas.url`                  | string  |         | `TRUENAS_URL`                  | `--truenas.url`                  | TrueNAS JSON-RPC websocket URL, for example wss://truenas.local/api/current. Must be wss: TrueNAS revokes API keys that are sent over plain ws (required)                                                   |
| `truenas.api-key`              | string  |         | `TRUENAS_API_KEY`              | `--truenas.api-key`              | TrueNAS API key. A user with the POOL_READ, DISK_READ, REPORTING_READ and ALERT_LIST_READ roles is enough (required) (secret)                                                                               |
| `truenas.tls-fingerprint`      | string  |         | `TRUENAS_TLS_FINGERPRINT`      | `--truenas.tls-fingerprint`      | SHA-256 fingerprint of the TrueNAS certificate, hex with or without colons. When set, the certificate is pinned instead of verified against the system CAs, which suits the default self-signed certificate |
| `truenas.insecure-skip-verify` | boolean | `false` | `TRUENAS_INSECURE_SKIP_VERIFY` | `--truenas.insecure-skip-verify` | Skip TLS certificate verification. Prefer tls-fingerprint                                                                                                                                                   |
| `truenas.timeout`              | string  | `30s`   | `TRUENAS_TIMEOUT`              | `--truenas.timeout`              | Timeout for connecting and for each API call                                                                                                                                                                |
| `poll.interval`                | string  | `1m`    | `POLL_INTERVAL`                | `--poll.interval`                | How often pools, disk temperatures, alerts and ARC are fetched                                                                                                                                              |
| `poll.smart-interval`          | string  | `15m`   | `POLL_SMART_INTERVAL`          | `--poll.smart-interval`          | SMART self-test poll interval; 0 disables. TrueNAS 25.04 rejects smart.test.results for read-only API keys, and 25.10 removes the call                                                                      |

<!-- configulator:end -->
