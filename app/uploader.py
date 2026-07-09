import shutil
from pathlib import Path
from fastapi import UploadFile
from abc import ABC, abstractmethod

from app.config import UPLOAD_DIR


class Uploader(ABC):
    @abstractmethod
    def upload(self, job_id: str, audio_file: UploadFile) -> tuple[Path, int]:
        """File that need to be uploaded."""
        pass

class LocalUploader(Uploader):
    def upload(self, job_id: str, audio_file: UploadFile) -> tuple[Path, int]:
        safe_filename = Path(audio_file.filename).name
        input_path = UPLOAD_DIR / f"{job_id}_{safe_filename}"
        with input_path.open("wb") as destination:
            shutil.copyfileobj(audio_file.file, destination)
        input_size_bytes = input_path.stat().st_size
        return input_path, input_size_bytes


def get_file_uploader():
    file_uploader = LocalUploader()
    yield file_uploader
