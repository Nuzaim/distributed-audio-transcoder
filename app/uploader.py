import os
import shutil
from uuid import UUID
from pathlib import Path
from fastapi import UploadFile
from abc import ABC, abstractmethod
from boto3 import client as aws_client
import botocore.exceptions
from botocore.client import Config

from app.config import DEFAULT_LOCAL_UPLOAD_DIR


class Uploader(ABC):
    @abstractmethod
    def upload(self, job_id: UUID, audio_file: UploadFile) -> tuple[Path, int]:
        """File that need to be uploaded."""
        pass

class LocalUploader(Uploader):
    def __init__(self, upload_dir = DEFAULT_LOCAL_UPLOAD_DIR):
        self.upload_dir = upload_dir
        self.upload_dir.mkdir(parents=True, exist_ok=True)

    def upload(self, job_id: UUID, audio_file: UploadFile) -> tuple[Path, int]:
        safe_filename = Path(audio_file.filename).name
        input_path = DEFAULT_LOCAL_UPLOAD_DIR / f"{job_id}_{safe_filename}"
        with input_path.open("wb") as destination:
            shutil.copyfileobj(audio_file.file, destination)
        input_size_bytes = input_path.stat().st_size
        absolute_input_path = input_path.resolve()
        return absolute_input_path, input_size_bytes

class S3Uploader(Uploader):
    def __init__(self, bucket_name, region_name, endpoint_url, aws_access_key_id, aws_secret_access_key):
        self.bucket_name = bucket_name
        self.s3_client = aws_client(
            "s3",
            region_name=region_name,
            endpoint_url=endpoint_url,
            aws_access_key_id=aws_access_key_id,
            aws_secret_access_key=aws_secret_access_key,
            config=Config(signature_version="s3v4"),
        )
        self.local_downloader = LocalUploader(Path("/tmp"))
    def get_audio_playback_url(self, s3_key: str, expires_in_seconds: int = 3600) -> str:
        """Generates a temporary URL that anyone can use to stream/download the file."""
        url = self.s3_client.generate_presigned_url(
            ClientMethod='get_object',
            Params={
                'Bucket': self.bucket_name,
                'Key': s3_key
            },
            ExpiresIn=expires_in_seconds
        )
        return url
    def upload(self, job_id: UUID, audio_file: UploadFile) -> tuple[Path, int]:
        upload_path = f"{job_id}/{audio_file.filename}"
        local_file_path, local_file_size_bytes = self.local_downloader.upload(job_id, audio_file)
        self.s3_client.upload_file(local_file_path, self.bucket_name, upload_path)
        s3_signed_url = self.get_audio_playback_url(upload_path)
        return s3_signed_url, local_file_size_bytes


def get_file_uploader():
    endpoint_url_host = os.environ.get("S3_ENDPOINT_URL", "http://localhost:9000")
    s3_uploader = S3Uploader(
        bucket_name="test-bucket",
        region_name="us-east-1",
        endpoint_url=endpoint_url_host,
        aws_access_key_id="minioadmin",
        aws_secret_access_key="minioadminpassword",
    )
    yield s3_uploader
