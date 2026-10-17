# Audio Transcoder TODO

This file tracks capabilities that are described by the intended design but are not implemented in the current project.

## Transcoder consumer review (`services/transcoder/main.go`)

- [ ] **P0 — Preserve job identity:** carry both `job_id` and `input_path` through the worker pool, validate them, and use `job_id` for output naming and the completion API request. The current consumer drops the ID.
- [ ] **P0 — Acknowledge after durable success:** retain each SQS receipt handle with its job, propagate storage and completion-API errors through the worker pool, and delete only successful jobs. Check per-message failures from batch deletion.
- [ ] **P0 — Validate messages safely:** handle absent or nil body/`content_type`, unsupported types, malformed JSON, and empty fields without panicking or dispatching an empty path. Choose an explicit retry or dead-letter policy for invalid messages.
- [ ] **P1 — Make shutdown single-owner:** stop polling on cancellation, have the producer close the work channel, define whether in-flight jobs drain or cancel, and wait for workers before exiting. Avoid closing the channel concurrently with sends and avoid `log.Fatal` in the receive loop.
- [ ] **P1 — Protect long-running jobs:** configure a visibility timeout and extend it while processing when jobs can outlast it, so a message is not delivered to another worker mid-transcode.
- [ ] **P1 — Retry transient queue and job failures:** add context-aware backoff for receive and delete errors, and route processing failures through the retry/dead-letter policy.
- [ ] **P2 — Configure consumer capacity:** validate the queue URL, receive batch size, long-poll duration, worker count, and channel capacity instead of hard-coding them in `main`.
- [ ] **P2 — Add useful consumer logs:** include job and SQS message IDs, and log validation, processing, and acknowledgement outcomes with structured fields.

## Job lifecycle

- Mark a job `processing` when a worker claims it.
- Record `failed` and `cancelled` states, failure details, and completion duration.
- Update attempt counts when processing is retried.
- Make state transitions and completion updates idempotent.
- Add a way to cancel an active or queued job.

## Reliable queue processing

- Acknowledge or delete a message only after the worker has completed processing and the completion update is durable.
- Add retry and backoff behavior around download, media processing, storage, and API update failures.
- Connect the configured dead-letter queue to an end-to-end retry policy.
- Prevent duplicate work when a message is delivered more than once.

## Consistent job creation

- Persist the job record before publishing the work message, or introduce an outbox so the database write and message publication cannot diverge.

## Output references and playback

- Store an object-storage key or stable output reference instead of a worker-local filesystem path.
- Provide a playback or download interface for completed HLS output.
- Apply access control and short-lived links when clients read generated files.

## Input validation and processing controls

- Validate file type, size, duration, and media contents before processing.
- Validate source references and job identifiers in worker messages.
- Check download responses before passing input to the media pipeline.
- Accept conversion options and support configurable output profiles.
- Add processing timeouts and clean up temporary files after each job.

## Configuration and storage

- Move database, queue, object-storage, bucket, and service settings into validated configuration.
- Move credentials out of source code and development defaults.
- Add database migrations.
- Use shared durable database storage when running multiple API instances.
- Define retention and cleanup policies for source audio, temporary files, and completed output.

## Worker operations

- Make worker concurrency configurable.
- Add worker health checks and graceful handling for stale or cancelled work.
- Make request timeouts and media-pipeline settings configurable.

## Observability

- Add job identifiers to every API and worker log entry.
- Add metrics for queue depth, job age, processing duration, retries, failures, and output size.
- Add tracing across the API service, message queue, worker, database, and object storage.
- Add alerts for queue growth, repeated failures, and dead-letter volume.

## Security and product decisions

- Add authentication and authorization for submitting, inspecting, cancelling, and playing jobs.
- Define supported input formats and audio codecs.
- Define output profiles, segment duration, and packaging rules.
- Define limits for file size, duration, concurrency, and processing time.
- Define source and output retention periods.

## Verification

- Add integration coverage for upload, queue delivery, processing, completion, failure, retry, and status streaming.
- Add tests for duplicate delivery and idempotent completion.
- Add end-to-end verification against the local development stack.
