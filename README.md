# Audio Transcoder

An audio-processing monorepo with an HTTP API for accepting uploads and a background worker that transcodes queued jobs with `ffmpeg`.

## Components

| Component | Purpose |
| --- | --- |
| `services/api` | FastAPI service that accepts audio uploads, records job metadata, and sends jobs to a queue. |
| `services/transcoder` | Go worker that consumes queued jobs and creates HLS output with `ffmpeg`. |
| `deploy/dev` | Local Docker Compose stack for the API, worker, ElasticMQ, and MinIO. |

The development stack uses ElasticMQ as an SQS-compatible queue and MinIO as an S3-compatible object store.

## Run Locally

Start the complete development environment from the repository root:

```sh
docker compose -f deploy/dev/docker-compose.yml up --build
```

Available services:

| Service | Address |
| --- | --- |
| API | `http://localhost:8000` |
| ElasticMQ UI | `http://localhost:3000` |
| MinIO API | `http://localhost:9000` |
| MinIO Console | `http://localhost:9001` |

Before uploading through the API, create a `test-bucket` bucket in the MinIO Console. The development credentials are `minioadmin` / `minioadminpassword`.

Stop the stack with:

```sh
docker compose -f deploy/dev/docker-compose.yml down
```

## API

The API exposes audio job endpoints under `/audio`.

Upload an audio file:

```sh
curl -X POST http://localhost:8000/audio/upload \
  -F 'audio_file=@/path/to/audio.mp3'
```

The response includes the job identifier. Retrieve its metadata with:

```sh
curl http://localhost:8000/audio/<job-id>
```

The API persists job metadata in SQLite and queues a message for the transcoder worker.

## Development

The Python project is rooted at `services/api` and uses `uv` for dependency management:

```sh
cd services/api
uv sync
uv run fastapi dev src/main.py
```

Run the Go worker directly from its service directory. This requires Go and `ffmpeg` to be installed:

```sh
cd services/transcoder
go run .
```

## Verification

```sh
docker compose -f deploy/dev/docker-compose.yml config
cd services/api && python3 -m py_compile src/*.py src/models/*.py src/routers/*.py src/schemas/*.py
cd services/transcoder && GOCACHE=/tmp/go-build go build .
```
