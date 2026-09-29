# HelloCalc

A tiny reference workload: a four-function calculator served by a single Go binary.

## Why it exists

HelloCalc is a canonical deployment payload for testing deployment systems across heterogeneous infrastructure: any cloud, VPS, bare metal, on-prem Linux, Docker, or Kubernetes. The product is intentionally trivial. What matters is that it is **deterministic, self-contained, observable, and easy to verify automatically**, and that the same artifact behaves the same everywhere.

Its entire runtime contract:

> an executable/container starts, binds to `HOST:PORT`, serves HTTP, logs JSON to stdout, answers health checks, and terminates cleanly on `SIGTERM`/`SIGINT`.

It has no database, no external calls, no cloud SDKs, no persistent state, and no dependencies outside the Go standard library.

## Architecture

```
Browser ──► hellocalc (single static binary)
              ├── /                embedded HTML/CSS/vanilla JS (go:embed)
              ├── /api/calculate   calculator API
              ├── /healthz /readyz health endpoints
              ├── /version         build metadata
              └── stdout           structured JSON logs
```

```
cmd/hellocalc/        entrypoint: config, signals, `healthcheck` and `version` subcommands
internal/calculator/  arithmetic (+ - * / % ^), no eval
internal/httpserver/  routes, middleware (request ID, logging, recovery, security headers), graceful shutdown
internal/config/      environment variable parsing
internal/buildinfo/   version/commit/build time (set via -ldflags)
web/                  UI assets, embedded into the binary
deploy/kubernetes.yaml
scripts/smoke-test.sh
```

## Run locally

Requires Go 1.24+.

```sh
make run                       # go run ./cmd/hellocalc → http://localhost:8080
make build && ./bin/hellocalc  # static binary with version metadata
PORT=9000 ./bin/hellocalc      # override the port
```

The binary also has two subcommands:

```sh
./bin/hellocalc version       # print build metadata as JSON
./bin/hellocalc healthcheck   # GET /healthz on the local server; exit 0 if healthy
```

## Tests

```sh
make test    # go test ./...
make check   # gofmt check, go vet, tests, build
```

## Docker

The image is multi-stage, built `FROM --platform=$BUILDPLATFORM` so it cross-compiles for `linux/amd64` and `linux/arm64` without emulation. The runtime image is `gcr.io/distroless/static-debian13:nonroot`: it has no shell and no package manager, and it runs as UID 65532. It has a built-in `HEALTHCHECK` that uses `hellocalc healthcheck`.

```sh
docker build -t hellocalc .
docker run --rm -p 8080:8080 hellocalc
curl http://localhost:8080/healthz
```

`make docker-build` also injects the version, commit and build time as build args (`VERSION`, `COMMIT`, `BUILD_TIME`). The same values are recorded as OCI labels. Multi-arch:

```sh
docker buildx build --platform linux/amd64,linux/arm64 -t <registry>/hellocalc:1.0.0 --push .
```

Compose (single service, read-only filesystem, all capabilities dropped):

```sh
docker compose up --build     # http://localhost:8080
```

## Configuration

All configuration is read from environment variables. No config files are used.

| Variable           | Default   | Description                                             |
|--------------------|-----------|---------------------------------------------------------|
| `HOST`             | `0.0.0.0` | Listen address (all interfaces)                         |
| `PORT`             | `8080`    | Listen port                                             |
| `LOG_LEVEL`        | `info`    | `debug`, `info`, `warn`, `error`                        |
| `SHUTDOWN_TIMEOUT` | `15s`     | Maximum time allowed for in-flight requests on shutdown |

Invalid values make the process exit with status 1 and a message on stderr.

## Operational endpoints

| Endpoint              | Success                                                                                  |
|-----------------------|------------------------------------------------------------------------------------------|
| `GET /healthz`        | `200 {"status":"ok"}` (liveness)                                                         |
| `GET /readyz`         | `200 {"status":"ready"}`; `503 {"status":"not ready"}` once shutdown begins             |
| `GET /version`        | `200 {"name","version","commit","buildTime","goVersion","os","arch"}`                    |
| `POST /api/calculate` | `200 {"result":50}` for `{"left":12.5,"operator":"*","right":4}`                         |

Calculator details:

- **Operators:** `+ - * / % ^` (`%` is floating-point modulo, `^` is power).
- **Errors:** errors are returned as `{"error":"..."}`:

  | Status | Cause |
  |--------|-------|
  | `400`  | Malformed JSON, unknown or missing fields, or an unsupported operator |
  | `413`  | Body larger than 4 KiB |
  | `415`  | `Content-Type` is not `application/json` |
  | `422`  | Division by zero, a result out of range, or a non-real result |

- **Wrong method:** returns `405` with an `Allow` header.

**Logging.** Every request produces one JSON log line on stdout with these fields:

- `timestamp`, `level`, `msg`
- `method`, `path`, `status`
- `duration_ms`, `bytes`
- `remote_addr`, `request_id`

Request bodies and query strings are never logged. Probe requests (`/healthz`, `/readyz`) are logged at `debug` level to keep production logs quiet.

**Request IDs.** An inbound `X-Request-ID` is reused if it is 1–128 visible ASCII characters. Otherwise a random ID is generated. Either way, the ID is returned in the `X-Request-ID` response header.

**Shutdown.** On `SIGTERM` or `SIGINT` the server:

1. marks itself not ready,
2. stops accepting connections,
3. waits up to `SHUTDOWN_TIMEOUT` for in-flight requests,
4. exits with status 0.

A second signal terminates the process immediately.

## Kubernetes

[`deploy/kubernetes.yaml`](deploy/kubernetes.yaml) is a vendor-neutral `Deployment` and `ClusterIP` `Service`. It includes:

- 2 replicas
- a rolling update with `maxUnavailable: 0`
- readiness and liveness probes on `/readyz` and `/healthz`
- 10m CPU / 16Mi memory requests and 250m CPU / 64Mi memory limits
- a non-root user, a read-only root filesystem, and all capabilities dropped

Push the image to a registry your cluster can pull from, then set the image and apply:

```sh
kubectl apply -f deploy/kubernetes.yaml
kubectl set image deployment/hellocalc hellocalc=<registry>/hellocalc:1.0.0
kubectl port-forward service/hellocalc 8080:80
```

## Smoke test

[`scripts/smoke-test.sh`](scripts/smoke-test.sh) needs only `sh` and `curl`. It checks:

- `/`, `/healthz`, `/readyz` and `/version`
- the calculator API, including division by zero and malformed JSON
- `X-Request-ID` propagation

The script exits `0` when every check passes and `1` otherwise.

```sh
BASE_URL=http://localhost:8080 ./scripts/smoke-test.sh
make smoke BASE_URL=https://hellocalc.example.internal
```

The script accepts these optional variables:

| Variable           | Default | Description                                                    |
|--------------------|---------|----------------------------------------------------------------|
| `WAIT_SECONDS`     | `10`    | How long to wait for `/readyz` to return 200 before testing    |
| `TIMEOUT`          | `5`     | Per-request timeout, in seconds                                |
| `EXPECTED_VERSION` | (unset) | If set, `/version` must report this version                    |
| `EXPECTED_COMMIT`  | (unset) | If set, `/version` must report this commit                     |

Use `EXPECTED_VERSION` and `EXPECTED_COMMIT` to assert that the expected artifact was deployed.

## License

[MIT](LICENSE)
