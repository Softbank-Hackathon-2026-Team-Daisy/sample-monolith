# sample-monolith — HelloCalc

A tiny reference workload: a four-function calculator served by a single Go binary.

## Team Daisy 연동 정보

이 레포는 Daisy 배포 시스템이 배포할 **"사용자의 앱 저장소"를 흉내 낸 모놀리스 샘플**이에요. 샘플 앱의 이름은 **HelloCalc**예요. 앱 동작은 일부러 단순하게 고정해 두었으니, 배포 시스템을 검증할 때 기준 앱으로 쓰면 돼요. 공통 맥락과 스키마 원본은 [`daisy/CLAUDE.md`](https://github.com/Softbank-Hackathon-2026-Team-Daisy/daisy/blob/main/CLAUDE.md)에 있어요.

### 앱 연결 요구사항 체크

| 요구사항 (daisy §2-1) | 위치 | 내용 |
|---|---|---|
| `Dockerfile` | [`Dockerfile`](Dockerfile) | 멀티스테이지 빌드, distroless 비루트(UID 65532), 셸 없음, amd64·arm64 지원, 포트 8080, `HEALTHCHECK` 내장 |
| `deploy.yaml` | [`deploy.yaml`](deploy.yaml) | 팀 스키마(§5, 9/29 초안) 그대로 |
| `/health` 엔드포인트 | `GET /health` | `200 {"status":"ok"}`. `/healthz`도 같은 응답 |
| `.github/workflows/` | [`.github/workflows/ci.yml`](.github/workflows/ci.yml) | N-01 이미지 파이프라인 |

### `deploy.yaml` ↔ 팀 스키마

```yaml
name: hellocalc
port: 8080              # 컨테이너 포트 (환경변수 PORT의 기본값과 같음)
healthcheck: /health    # ALB 대상 그룹 · Cloud Run 시작 프로브 · Docker 헬스체크
env:                    # 모두 선택값. 넘기지 않으면 기본값 사용
  - LOG_LEVEL           # debug | info | warn | error (기본 info)
  - SHUTDOWN_TIMEOUT    # 종료 시 처리 중 요청 대기 한도 (기본 15s)
secrets: []             # 비밀값 없음
database: false         # DB 없음
```

- `HOST`(기본 `0.0.0.0`)와 `PORT`(기본 `8080`)도 환경변수로 바꿀 수 있어요. `PORT`를 바꾸면 `deploy.yaml`의 `port`도 같이 바꿔야 해요.
- 스키마가 확정되거나 바뀌면 이 파일도 맞춰 주세요. 스키마는 파트 사이의 약속이라, 바꿀 때는 전 파트에 공유해요.

### N-01 GitHub Actions 파이프라인 (`ci.yml`)

```
PR        : test(make check) → 이미지 빌드(amd64) → 컨테이너 스모크 테스트        (푸시 없음)
main push : 위 과정 → amd64·arm64 이미지 빌드 → ghcr.io 푸시 → 배포 서비스에 이벤트 전달
```

| 항목 | 값 |
|---|---|
| 이미지 | `ghcr.io/softbank-hackathon-2026-team-daisy/sample-monolith:<커밋 해시 40자>` |
| 태그 규칙 | 항상 커밋 해시(`github.sha`). `latest`는 쓰지 않아요 |
| 레지스트리 | GHCR (`GITHUB_TOKEN` 사용, 별도 비밀값 없음). 팀 레지스트리가 정해지면 `image` 잡만 바꾸면 돼요 |
| 이벤트 전달 | 저장소 Secret `DAISY_WEBHOOK_URL`이 있으면 아래 JSON을 POST해요. 없으면 notice만 남기고 건너뛰어요 |

```json
{
  "repository": "Softbank-Hackathon-2026-Team-Daisy/sample-monolith",
  "commit": "<40자 커밋 해시>",
  "ref": "refs/heads/main",
  "image": "ghcr.io/softbank-hackathon-2026-team-daisy/sample-monolith:<커밋 해시>",
  "run_url": "https://github.com/.../actions/runs/<id>"
}
```

- server 파트(하은현): 웹훅 수신 API가 정해지면 Secret `DAISY_WEBHOOK_URL`에 주소를 넣고, 필드가 다르면 `ci.yml`의 `notify` 잡에서 payload를 맞춰 주세요.
- 이미지를 처음 푸시하면 GHCR 패키지가 비공개로 만들어질 수 있어요. 인증 없이 이미지를 받아야 하는 환경(온프레미스 Docker, Cloud Run 등)이라면 패키지 설정에서 공개로 바꾸거나, 그 환경에 레지스트리 인증을 넣어 주세요.

### 배포 검증 방법

배포한 뒤에는 대상 환경 주소로 스모크 테스트를 돌려요. `sh`와 `curl`만 있으면 돼요. 모두 통과하면 exit 0, 하나라도 실패하면 exit 1이에요.

```sh
BASE_URL=https://<배포 주소> EXPECTED_COMMIT=<배포한 커밋 해시> ./scripts/smoke-test.sh
```

| 확인 대상 | 방법 |
|---|---|
| 떠 있는지 / 받을 준비가 됐는지 | `GET /health` → 200 / `GET /readyz` → 200. 종료가 시작되면 `/readyz`는 503 |
| 의도한 이미지가 배포됐는지 | `GET /version`의 `commit`이 이미지 태그(커밋 해시)와 같은지. 스모크 테스트에서는 `EXPECTED_COMMIT`로 확인 |
| 앱 기능 | `POST /api/calculate` (`{"left":12.5,"operator":"*","right":4}` → `{"result":50}`) |
| 로그 수집 | 요청마다 stdout에 JSON 한 줄. `X-Request-ID` 헤더로 요청을 추적할 수 있어요 |
| 종료 처리 | SIGTERM → 새 연결 거부 → 처리 중 요청 완료 → exit 0 (`docker stop`으로 확인) |
| 리소스 | 유휴 메모리 약 11 MiB, 로컬 이미지 약 17 MB. 작은 인스턴스로 충분해요 |

알려진 한계: 종료 전 대기 설정(preStop 등)이 없어요. 그래서 로드밸런서나 Kubernetes가 대상에서 빼기 전 몇 초 동안 들어온 요청은 연결 거부될 수 있어요. 무중단 배포를 검증할 때는 플랫폼 쪽 등록 해제 지연(deregistration delay 등)을 함께 확인해 주세요.

### 협업 규칙 (daisy `CONTRIBUTING.md` 요약)

- `main`에 직접 push하지 않고 PR로 올려요. 리뷰 1명 승인 후 squash merge해요.
- 브랜치는 `{파트}/{타입}-{설명}` 형식이에요 (예: `ci/feat-image-pipeline`). 커밋은 `feat(ci): ...`처럼 적어요.
- `.env`, 키 파일 같은 비밀값은 커밋하지 않아요. 필요한 값은 GitHub Actions Secrets에 넣어요.
- 담당: 앱 하은현, Actions(N-01) 김도영 `[미정]`

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
              ├── /health /healthz /readyz  health endpoints
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
./bin/hellocalc healthcheck   # GET /health on the local server; exit 0 if healthy
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
| `GET /health`, `GET /healthz` | `200 {"status":"ok"}` (liveness)                                                |
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

Request bodies and query strings are never logged. Probe requests (`/health`, `/healthz`, `/readyz`) are logged at `debug` level to keep production logs quiet.

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

- `/`, `/health`, `/healthz`, `/readyz` and `/version`
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
