#!/usr/bin/env python3
"""
Distributed Audio Transcoder Test Script

Tests the distributed audio transcoder application on test audio data.
Supports:
  - Generating synthetic test audio files (MP3, WAV, AAC, Silence)
  - Pre-flight service health checks (API, MinIO, ElasticMQ)
  - Automatic MinIO bucket initialization ('test-bucket')
  - End-to-end testing: Upload -> SQS -> Transcoder -> MinIO -> API status
  - Playlist (.m3u8) & segment (.ts) output verification
  - Concurrent testing of worker pool capacity
  - Direct Go worker test execution
"""

import argparse
import concurrent.futures
import datetime
import hashlib
import hmac
import json
import os
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Dict, List, Optional, Tuple

# Terminal colors
class Colors:
    GREEN = "\033[92m"
    RED = "\033[91m"
    YELLOW = "\033[93m"
    BLUE = "\033[94m"
    MAGENTA = "\033[95m"
    CYAN = "\033[96m"
    BOLD = "\033[1m"
    RESET = "\033[0m"

    @classmethod
    def disable(cls):
        cls.GREEN = ""
        cls.RED = ""
        cls.YELLOW = ""
        cls.BLUE = ""
        cls.MAGENTA = ""
        cls.CYAN = ""
        cls.BOLD = ""
        cls.RESET = ""


# MinIO / S3 SigV4 client using Python standard library
class MinioClient:
    def __init__(
        self,
        endpoint: str = "http://localhost:9000",
        access_key: str = "minioadmin",
        secret_key: str = "minioadminpassword",
        region: str = "us-east-1",
    ):
        self.endpoint = endpoint.rstrip("/")
        self.access_key = access_key
        self.secret_key = secret_key
        self.region = region

    def _sign(self, key: bytes, msg: str) -> bytes:
        return hmac.new(key, msg.encode("utf-8"), hashlib.sha256).digest()

    def _get_signature_key(self, date_stamp: str) -> bytes:
        k_date = self._sign(("AWS4" + self.secret_key).encode("utf-8"), date_stamp)
        k_region = self._sign(k_date, self.region)
        k_service = self._sign(k_region, "s3")
        k_signing = self._sign(k_service, "aws4_request")
        return k_signing

    def request(
        self, method: str, path: str, body: bytes = b"", headers: Optional[Dict[str, str]] = None
    ) -> urllib.request.Request:
        if headers is None:
            headers = {}
        now = datetime.datetime.now(datetime.timezone.utc)
        amz_date = now.strftime("%Y%m%dT%H%M%SZ")
        date_stamp = now.strftime("%Y%m%d")

        parsed = urllib.parse.urlparse(self.endpoint)
        host = parsed.netloc
        payload_hash = hashlib.sha256(body).hexdigest()

        headers["host"] = host
        headers["x-amz-date"] = amz_date
        headers["x-amz-content-sha256"] = payload_hash

        canonical_uri = path if path.startswith("/") else "/" + path
        canonical_querystring = ""

        sorted_header_keys = sorted(k.lower() for k in headers.keys())
        canonical_headers = "".join(f"{k}:{headers[k].strip()}\n" for k in sorted_header_keys)
        signed_headers = ";".join(sorted_header_keys)

        canonical_request = (
            f"{method}\n{canonical_uri}\n{canonical_querystring}\n"
            f"{canonical_headers}\n{signed_headers}\n{payload_hash}"
        )

        algorithm = "AWS4-HMAC-SHA256"
        credential_scope = f"{date_stamp}/{self.region}/s3/aws4_request"
        string_to_sign = (
            f"{algorithm}\n{amz_date}\n{credential_scope}\n"
            f"{hashlib.sha256(canonical_request.encode('utf-8')).hexdigest()}"
        )

        signing_key = self._get_signature_key(date_stamp)
        signature = hmac.new(
            signing_key, string_to_sign.encode("utf-8"), hashlib.sha256
        ).hexdigest()

        auth_header = (
            f"{algorithm} Credential={self.access_key}/{credential_scope}, "
            f"SignedHeaders={signed_headers}, Signature={signature}"
        )
        headers["Authorization"] = auth_header

        url = f"{self.endpoint}{canonical_uri}"
        req = urllib.request.Request(
            url,
            data=body if method in ["PUT", "POST"] else None,
            headers=headers,
            method=method,
        )
        return req

    def ensure_bucket(self, bucket_name: str) -> bool:
        """Checks if bucket exists; creates it if missing."""
        head_req = self.request("HEAD", f"/{bucket_name}")
        try:
            with urllib.request.urlopen(head_req, timeout=5) as resp:
                return True
        except urllib.error.HTTPError as e:
            if e.code == 404:
                # Create bucket
                put_req = self.request("PUT", f"/{bucket_name}")
                try:
                    with urllib.request.urlopen(put_req, timeout=5) as resp:
                        return True
                except Exception as create_err:
                    print(f"{Colors.RED}Failed to create bucket {bucket_name}: {create_err}{Colors.RESET}")
                    return False
            else:
                print(f"{Colors.YELLOW}Warning checking bucket {bucket_name}: HTTP {e.code}{Colors.RESET}")
                return False
        except Exception as e:
            print(f"{Colors.RED}Error connecting to MinIO at {self.endpoint}: {e}{Colors.RESET}")
            return False

    def check_object_exists(self, bucket_name: str, object_key: str) -> bool:
        """Checks if an object exists in MinIO."""
        path = f"/{bucket_name}/{object_key.lstrip('/')}"
        head_req = self.request("HEAD", path)
        try:
            with urllib.request.urlopen(head_req, timeout=5) as resp:
                return resp.status == 200
        except urllib.error.HTTPError:
            return False
        except Exception:
            return False

    def fetch_object(self, bucket_name: str, object_key: str) -> Optional[bytes]:
        """Downloads an object from MinIO."""
        path = f"/{bucket_name}/{object_key.lstrip('/')}"
        get_req = self.request("GET", path)
        try:
            with urllib.request.urlopen(get_req, timeout=5) as resp:
                return resp.read()
        except Exception:
            return None


def generate_test_audio(output_dir: Path, force: bool = False) -> List[Path]:
    """Generates synthetic audio test files if ffmpeg is available."""
    output_dir.mkdir(parents=True, exist_ok=True)
    files_to_generate = [
        (
            output_dir / "sample_5s.mp3",
            ["ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=5", "-c:a", "libmp3lame", str(output_dir / "sample_5s.mp3")],
            "5-second MP3 tone (440Hz sine wave, tests single-segment HLS)",
        ),
        (
            output_dir / "sample_25s.wav",
            ["ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=523:duration=25", str(output_dir / "sample_25s.wav")],
            "25-second WAV tone (tests multi-segment HLS with 20s chunking)",
        ),
        (
            output_dir / "sample_10s.aac",
            ["ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=659:duration=10", "-c:a", "aac", str(output_dir / "sample_10s.aac")],
            "10-second AAC tone (tests AAC format copy)",
        ),
        (
            output_dir / "sample_silent_3s.mp3",
            ["ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "3", "-c:a", "libmp3lame", str(output_dir / "sample_silent_3s.mp3")],
            "3-second silent MP3 (tests edge case: audio with no sound)",
        ),
    ]

    generated = []
    if not shutil.which("ffmpeg"):
        print(f"{Colors.YELLOW}[!] ffmpeg not found in PATH; skipping audio generation.{Colors.RESET}")
        return [f[0] for f in files_to_generate if f[0].exists()]

    for target_file, cmd, description in files_to_generate:
        if target_file.exists() and not force:
            generated.append(target_file)
            continue
        try:
            subprocess.run(cmd, check=True, capture_output=True)
            size_kb = target_file.stat().st_size / 1024
            print(f"{Colors.GREEN}[+] Generated {target_file.name}{Colors.RESET} ({size_kb:.1f} KB) - {description}")
            generated.append(target_file)
        except subprocess.CalledProcessError as e:
            print(f"{Colors.RED}[-] Failed generating {target_file.name}: {e.stderr.decode()[:200]}{Colors.RESET}")

    return generated


def check_services(api_url: str, minio_url: str, sqs_url: str) -> Dict[str, bool]:
    """Checks reachability of the services in the stack."""
    results = {}

    # Check API
    try:
        req = urllib.request.Request(f"{api_url.rstrip('/')}/docs", headers={"User-Agent": "TestClient"})
        with urllib.request.urlopen(req, timeout=3) as resp:
            results["API Service"] = resp.status in (200, 404)
    except Exception:
        results["API Service"] = False

    # Check MinIO
    try:
        req = urllib.request.Request(f"{minio_url.rstrip('/')}/minio/health/live")
        with urllib.request.urlopen(req, timeout=3) as resp:
            results["MinIO Storage"] = resp.status == 200
    except Exception:
        results["MinIO Storage"] = False

    # Check ElasticMQ
    try:
        req = urllib.request.Request(sqs_url)
        with urllib.request.urlopen(req, timeout=3) as resp:
            results["ElasticMQ Queue"] = resp.status in (200, 400, 404)
    except Exception:
        results["ElasticMQ Queue"] = False

    return results


def upload_audio(api_url: str, audio_file: Path) -> Tuple[int, Dict]:
    """Uploads an audio file to POST /audio/upload using standard library."""
    boundary = f"----WebKitFormBoundary{os.urandom(16).hex()}"
    filename = audio_file.name

    with open(audio_file, "rb") as f:
        file_bytes = f.read()

    # Determine MIME type
    if filename.endswith(".mp3"):
        mime = "audio/mpeg"
    elif filename.endswith(".wav"):
        mime = "audio/wav"
    elif filename.endswith(".aac"):
        mime = "audio/aac"
    else:
        mime = "application/octet-stream"

    body = (
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="audio_file"; filename="{filename}"\r\n'
        f"Content-Type: {mime}\r\n\r\n"
    ).encode("utf-8") + file_bytes + f"\r\n--{boundary}--\r\n".encode("utf-8")

    upload_url = f"{api_url.rstrip('/')}/audio/upload"
    req = urllib.request.Request(
        upload_url,
        data=body,
        headers={
            "Content-Type": f"multipart/form-data; boundary={boundary}",
            "Content-Length": str(len(body)),
        },
        method="POST",
    )

    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            response_json = json.loads(resp.read().decode("utf-8"))
            return resp.status, response_json
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8", errors="replace")
        try:
            return e.code, json.loads(err_body)
        except Exception:
            return e.code, {"error": err_body}
    except Exception as e:
        return 0, {"error": str(e)}


def get_audio_job(api_url: str, job_id: str) -> Tuple[int, Dict]:
    """Fetches job metadata from GET /audio/{job_id}."""
    url = f"{api_url.rstrip('/')}/audio/{job_id}"
    req = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        return e.code, {}
    except Exception as e:
        return 0, {"error": str(e)}


def validate_playlist(content: str) -> Tuple[bool, List[str], List[str]]:
    """Validates HLS playlist structure and returns segment filenames."""
    errors = []
    segments = []

    lines = [line.strip() for line in content.splitlines() if line.strip()]
    if not lines or lines[0] != "#EXTM3U":
        errors.append("Missing #EXTM3U header at start of playlist")

    has_target_duration = any(line.startswith("#EXT-X-TARGETDURATION") for line in lines)
    if not has_target_duration:
        errors.append("Missing #EXT-X-TARGETDURATION directive")

    has_endlist = any(line == "#EXT-X-ENDLIST" for line in lines)
    if not has_endlist:
        errors.append("Missing #EXT-X-ENDLIST tag")

    for line in lines:
        if not line.startswith("#") and line.endswith(".ts"):
            segments.append(line)

    if not segments:
        errors.append("Playlist does not contain any .ts segment references")

    return len(errors) == 0, segments, errors


def test_single_audio(
    api_url: str,
    minio_client: MinioClient,
    bucket_name: str,
    audio_path: Path,
    timeout: int = 60,
    poll_interval: float = 1.0,
    repo_root: Optional[Path] = None,
) -> Dict:
    """Executes an end-to-end test on a single audio file."""
    start_time = time.time()
    filename = audio_path.name
    size_bytes = audio_path.stat().st_size
    result = {
        "file": filename,
        "size_kb": f"{size_bytes / 1024:.1f}",
        "job_id": None,
        "upload_status": None,
        "final_status": None,
        "duration_sec": 0.0,
        "segments": 0,
        "passed": False,
        "error": None,
    }

    print(f"\n{Colors.BOLD}{Colors.CYAN}---> Testing: {filename} ({size_bytes / 1024:.1f} KB){Colors.RESET}")

    # 1. Upload audio
    print(f"  [{Colors.BLUE}UPLOAD{Colors.RESET}] Submitting to {api_url}/audio/upload...")
    upload_status, upload_resp = upload_audio(api_url, audio_path)
    result["upload_status"] = upload_status

    if upload_status not in (200, 201):
        err = upload_resp.get("error") or upload_resp.get("detail") or "Upload rejected"
        result["error"] = f"Upload failed (HTTP {upload_status}): {err}"
        print(f"  [{Colors.RED}FAILED{Colors.RESET}] {result['error']}")
        result["duration_sec"] = round(time.time() - start_time, 2)
        return result

    job_id = upload_resp.get("id")
    result["job_id"] = job_id
    initial_status = upload_resp.get("status")
    print(f"  [{Colors.GREEN}QUEUED{Colors.RESET}] Job ID: {job_id} | Initial status: {initial_status}")

    # 2. Poll for completion
    poll_start = time.time()
    job_data = upload_resp
    while time.time() - poll_start < timeout:
        elapsed = int(time.time() - poll_start)
        status_code, fetched = get_audio_job(api_url, job_id)
        if status_code == 200:
            job_data = fetched
            current_status = job_data.get("status")
            output_path = job_data.get("output_path")

            if current_status == "completed" or output_path:
                print(f"\r  [{Colors.GREEN}PROCESSING{Colors.RESET}] Status: completed after {elapsed}s       ")
                result["final_status"] = "completed"
                break
            elif current_status in ("failed", "cancelled"):
                result["final_status"] = current_status
                result["error"] = f"Job reached terminal status: {current_status}"
                print(f"\r  [{Colors.RED}FAILED{Colors.RESET}] Job ended with status: {current_status}        ")
                break
            else:
                sys.stdout.write(f"\r  [{Colors.YELLOW}WAITING{Colors.RESET}] Status: {current_status} ({elapsed}s)...")
                sys.stdout.flush()

        time.sleep(poll_interval)
    else:
        result["final_status"] = "timeout"
        result["error"] = f"Job timed out after {timeout} seconds"
        print(f"\n  [{Colors.RED}TIMEOUT{Colors.RESET}] {result['error']}")
        result["duration_sec"] = round(time.time() - start_time, 2)
        return result

    result["duration_sec"] = round(time.time() - start_time, 2)

    if result["final_status"] != "completed":
        return result

    output_path = job_data.get("output_path")
    print(f"  [{Colors.BLUE}VERIFY{Colors.RESET}] Worker reported output: {output_path}")

    # 3. Verify HLS Output
    # Check local filesystem (runtime/hls/{job_id}/playlist.m3u8)
    playlist_text = None
    if repo_root:
        local_hls_dir = repo_root / "runtime" / "hls" / job_id
        local_playlist = local_hls_dir / "playlist.m3u8"
        if local_playlist.exists():
            print(f"  [{Colors.GREEN}LOCAL{Colors.RESET}] Verified local playlist at: {local_playlist}")
            try:
                playlist_text = local_playlist.read_text(encoding="utf-8")
            except Exception as e:
                print(f"  [{Colors.YELLOW}WARN{Colors.RESET}] Unable to read local playlist: {e}")

    # Check MinIO S3 object
    minio_playlist_key = f"{job_id}/playlist.m3u8"
    s3_playlist_bytes = minio_client.fetch_object(bucket_name, minio_playlist_key)
    if s3_playlist_bytes:
        print(f"  [{Colors.GREEN}MINIO{Colors.RESET}] Verified S3 object: {bucket_name}/{minio_playlist_key}")
        if not playlist_text:
            playlist_text = s3_playlist_bytes.decode("utf-8", errors="replace")
    else:
        print(f"  [{Colors.YELLOW}WARN{Colors.RESET}] Could not fetch {bucket_name}/{minio_playlist_key} from MinIO")

    if not playlist_text:
        result["error"] = "Playlist file could not be retrieved from local runtime or MinIO"
        print(f"  [{Colors.RED}FAILED{Colors.RESET}] {result['error']}")
        return result

    # Validate playlist syntax and segments
    is_valid, segments, errors = validate_playlist(playlist_text)
    result["segments"] = len(segments)

    if not is_valid:
        result["error"] = f"Invalid playlist: {', '.join(errors)}"
        print(f"  [{Colors.RED}FAILED{Colors.RESET}] {result['error']}")
        return result

    print(f"  [{Colors.GREEN}PLAYLIST{Colors.RESET}] Valid HLS playlist with {len(segments)} segments: {', '.join(segments)}")

    # Verify segments in MinIO
    missing_segments = []
    for segment in segments:
        seg_key = f"{job_id}/{segment}"
        if not minio_client.check_object_exists(bucket_name, seg_key):
            # Check local
            if repo_root and not (repo_root / "runtime" / "hls" / job_id / segment).exists():
                missing_segments.append(segment)

    if missing_segments:
        result["error"] = f"Missing segments: {', '.join(missing_segments)}"
        print(f"  [{Colors.RED}FAILED{Colors.RESET}] {result['error']}")
        return result

    result["passed"] = True
    print(f"  [{Colors.GREEN}PASSED{Colors.RESET}] Completed in {result['duration_sec']}s")
    return result


def run_go_transcoder_tests(repo_root: Path) -> bool:
    """Runs Go unit & integration tests in services/transcoder."""
    print(f"\n{Colors.BOLD}{Colors.CYAN}=== Running Go Transcoder Tests (services/transcoder) ==={Colors.RESET}")
    transcoder_dir = repo_root / "services" / "transcoder"
    if not transcoder_dir.exists():
        print(f"{Colors.RED}services/transcoder directory not found{Colors.RESET}")
        return False

    cmd = ["go", "test", "-v", "./..."]
    try:
        proc = subprocess.run(cmd, cwd=str(transcoder_dir), text=True, capture_output=True)
        print(proc.stdout)
        if proc.returncode == 0:
            print(f"{Colors.GREEN}[+] All Go transcoder tests passed!{Colors.RESET}")
            return True
        else:
            print(f"{Colors.RED}[-] Go transcoder tests failed:{Colors.RESET}\n{proc.stderr}")
            return False
    except Exception as e:
        print(f"{Colors.RED}[-] Error executing go test: {e}{Colors.RESET}")
        return False


def main():
    parser = argparse.ArgumentParser(
        description="Test the Distributed Audio Transcoder app on test audio data."
    )
    parser.add_argument(
        "--file", "-f", type=Path, help="Specific audio file to test."
    )
    parser.add_argument(
        "--dir", "-d", type=Path, default=Path("test_data"), help="Directory containing test audio files (default: test_data)."
    )
    parser.add_argument(
        "--api-url", default="http://localhost:8000", help="API base URL (default: http://localhost:8000)."
    )
    parser.add_argument(
        "--minio-url", default="http://localhost:9000", help="MinIO S3 endpoint URL (default: http://localhost:9000)."
    )
    parser.add_argument(
        "--sqs-url", default="http://localhost:9324", help="ElasticMQ SQS URL (default: http://localhost:9324)."
    )
    parser.add_argument(
        "--bucket", default="test-bucket", help="MinIO bucket name (default: test-bucket)."
    )
    parser.add_argument(
        "--timeout", "-t", type=int, default=60, help="Maximum seconds to wait for job completion (default: 60)."
    )
    parser.add_argument(
        "--poll-interval", type=float, default=1.0, help="Status polling interval in seconds (default: 1.0)."
    )
    parser.add_argument(
        "--concurrent", "-c", type=int, default=1, help="Number of concurrent test runs (default: 1)."
    )
    parser.add_argument(
        "--generate", action="store_true", help="Force regenerate sample test audio files."
    )
    parser.add_argument(
        "--generate-only", action="store_true", help="Only generate sample audio files and exit."
    )
    parser.add_argument(
        "--check", action="store_true", help="Only check services health and exit."
    )
    parser.add_argument(
        "--go-tests", action="store_true", help="Run Go worker unit & integration tests."
    )
    parser.add_argument(
        "--no-color", action="store_true", help="Disable colored terminal output."
    )

    args = parser.parse_args()

    if args.no_color or not sys.stdout.isatty():
        Colors.disable()

    # Determine workspace root
    script_dir = Path(__file__).resolve().parent
    repo_root = script_dir.parent

    print(f"{Colors.BOLD}{Colors.MAGENTA}=================================================={Colors.RESET}")
    print(f"{Colors.BOLD}{Colors.MAGENTA}     DISTRIBUTED AUDIO TRANSCODER TEST SUITE      {Colors.RESET}")
    print(f"{Colors.BOLD}{Colors.MAGENTA}=================================================={Colors.RESET}")

    # Mode 1: Go tests only
    if args.go_tests:
        success = run_go_transcoder_tests(repo_root)
        sys.exit(0 if success else 1)

    # Mode 2: Test audio generation
    test_data_dir = args.dir if args.dir.is_absolute() else repo_root / args.dir
    if args.generate or args.generate_only or not test_data_dir.exists() or not any(test_data_dir.glob("*.*")):
        print(f"\n{Colors.BOLD}[*] Preparing test data in {test_data_dir}...{Colors.RESET}")
        generated = generate_test_audio(test_data_dir, force=args.generate)
        if args.generate_only:
            print(f"\n{Colors.GREEN}[+] Test data generation complete ({len(generated)} files).{Colors.RESET}")
            sys.exit(0)

    # Mode 3: Service Health Check
    print(f"\n{Colors.BOLD}[*] Checking service connectivity...{Colors.RESET}")
    services = check_services(args.api_url, args.minio_url, args.sqs_url)
    all_healthy = True
    for svc, is_up in services.items():
        status_label = f"{Colors.GREEN}ONLINE{Colors.RESET}" if is_up else f"{Colors.RED}OFFLINE{Colors.RESET}"
        print(f"  • {svc:<18}: {status_label}")
        if not is_up:
            all_healthy = False

    if args.check:
        sys.exit(0 if all_healthy else 1)

    if not services.get("API Service"):
        print(f"\n{Colors.RED}[!] Error: API service at {args.api_url} is not reachable.{Colors.RESET}")
        print(f"{Colors.YELLOW}    To start the local environment, run:{Colors.RESET}")
        print("    docker compose -f deploy/dev/docker-compose.yml up -d --build\n")
        print(f"{Colors.YELLOW}    Or run Go transcoder tests standalone with:{Colors.RESET}")
        print("    python3 scripts/test_app.py --go-tests\n")
        sys.exit(1)

    # Initialize MinIO Bucket
    minio_client = MinioClient(endpoint=args.minio_url)
    print(f"\n{Colors.BOLD}[*] Ensuring MinIO bucket '{args.bucket}' exists...{Colors.RESET}")
    if minio_client.ensure_bucket(args.bucket):
        print(f"  • MinIO bucket '{args.bucket}': {Colors.GREEN}READY{Colors.RESET}")
    else:
        print(f"  • MinIO bucket '{args.bucket}': {Colors.YELLOW}CHECK WARNING{Colors.RESET}")

    # Gather test files
    if args.file:
        test_files = [args.file if args.file.is_absolute() else Path.cwd() / args.file]
        if not test_files[0].exists():
            print(f"{Colors.RED}[!] Specified test file does not exist: {test_files[0]}{Colors.RESET}")
            sys.exit(1)
    else:
        audio_extensions = {".mp3", ".wav", ".aac", ".flac", ".ogg", ".m4a"}
        test_files = sorted(
            [f for f in test_data_dir.iterdir() if f.suffix.lower() in audio_extensions]
        )
        if not test_files:
            print(f"{Colors.RED}[!] No audio test files found in {test_data_dir}{Colors.RESET}")
            sys.exit(1)

    print(f"\n{Colors.BOLD}[*] Found {len(test_files)} test audio file(s):{Colors.RESET}")
    for tf in test_files:
        print(f"  • {tf.name:<25} ({tf.stat().st_size / 1024:.1f} KB)")

    # Execute tests
    test_results = []
    if args.concurrent > 1:
        print(f"\n{Colors.BOLD}[*] Executing concurrent tests (concurrency={args.concurrent})...{Colors.RESET}")
        with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrent) as executor:
            future_to_file = {
                executor.submit(
                    test_single_audio,
                    args.api_url,
                    minio_client,
                    args.bucket,
                    tf,
                    args.timeout,
                    args.poll_interval,
                    repo_root,
                ): tf
                for tf in test_files
            }
            for future in concurrent.futures.as_completed(future_to_file):
                test_results.append(future.result())
    else:
        for tf in test_files:
            res = test_single_audio(
                args.api_url,
                minio_client,
                args.bucket,
                tf,
                args.timeout,
                args.poll_interval,
                repo_root,
            )
            test_results.append(res)

    # Print Summary Table
    print(f"\n{Colors.BOLD}{Colors.MAGENTA}=================================================={Colors.RESET}")
    print(f"{Colors.BOLD}{Colors.MAGENTA}                  TEST SUMMARY                    {Colors.RESET}")
    print(f"{Colors.BOLD}{Colors.MAGENTA}=================================================={Colors.RESET}")
    print(
        f"{'File':<24} {'Size':<10} {'Status':<12} {'Segments':<10} {'Duration':<10} {'Result'}"
    )
    print("-" * 75)

    passed_count = 0
    for r in test_results:
        status_text = r["final_status"] or r["upload_status"] or "error"
        if r["passed"]:
            res_str = f"{Colors.GREEN}PASS{Colors.RESET}"
            passed_count += 1
        else:
            res_str = f"{Colors.RED}FAIL{Colors.RESET}"
        print(
            f"{r['file']:<24} {r['size_kb'] + ' KB':<10} {str(status_text):<12} {str(r['segments']):<10} {str(r['duration_sec']) + 's':<10} {res_str}"
        )
        if r.get("error"):
            print(f"  └─ {Colors.RED}Error: {r['error']}{Colors.RESET}")

    print("-" * 75)
    total_count = len(test_results)
    if passed_count == total_count:
        print(
            f"{Colors.BOLD}{Colors.GREEN}ALL TESTS PASSED ({passed_count}/{total_count})! Transcoder pipeline verified.{Colors.RESET}\n"
        )
        sys.exit(0)
    else:
        print(
            f"{Colors.BOLD}{Colors.RED}TESTS FAILED ({passed_count}/{total_count} passed).{Colors.RESET}\n"
        )
        sys.exit(1)


if __name__ == "__main__":
    main()
