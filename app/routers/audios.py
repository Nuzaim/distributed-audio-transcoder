import shutil
from uuid import uuid7
from pathlib import Path
from fastapi import APIRouter, Depends, File, Query, UploadFile, status, exceptions
from sqlalchemy import select
from sqlalchemy.orm import Session as DbSession

from app.schemas.audio import QueuedAudioJob
from app.models.audio import TranscodeJob
from app.database import get_session
from app.config import UPLOAD_DIR


audio_router = APIRouter(prefix="/audio", tags=["audio"])

@audio_router.get("/{audio_id}", response_model=QueuedAudioJob, status_code=status.HTTP_200_OK)
def get_audio(
    audio_id: str,
    session: DbSession = Depends(get_session),
):
    stmt = select(TranscodeJob).where(TranscodeJob.id==audio_id)
    result = session.execute(stmt).one()
    return result[0]

@audio_router.post("/upload", response_model=QueuedAudioJob, status_code=status.HTTP_201_CREATED)
def upload_audio(
    audio_file: UploadFile = File(...),
    session: DbSession = Depends(get_session),
):
    if not audio_file.filename:
        raise exceptions.HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail="Uploaded file must have a filename",
        )

    job_id = str(uuid7())
    safe_filename = Path(audio_file.filename).name
    input_path = UPLOAD_DIR / f"{job_id}_{safe_filename}"

    with input_path.open("wb") as destination:
        shutil.copyfileobj(audio_file.file, destination)

    input_size_bytes = input_path.stat().st_size
    job = TranscodeJob(
        id=job_id,
        input_path=str(input_path),
        input_mime_type=audio_file.content_type,
        input_size_bytes=input_size_bytes,
    )
    session.add(job)
    session.commit()
    session.refresh(job)
    return job
