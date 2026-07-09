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
from app.uploader import LocalUploader, Uploader, get_file_uploader


audio_router = APIRouter(prefix="/audio", tags=["audio"])

@audio_router.get("/{audio_id}", response_model=QueuedAudioJob, status_code=status.HTTP_200_OK)
def get_audio(
    audio_id: str,
    session: DbSession = Depends(get_session),
):
    stmt = select(TranscodeJob).where(TranscodeJob.id==audio_id)
    job = session.execute(stmt).scalar_one_or_none()
    if not job:
        raise exceptions.HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Audio job with ID {audio_id} not found"
        )
    return job

@audio_router.post("/upload", response_model=QueuedAudioJob, status_code=status.HTTP_201_CREATED)
def upload_audio(
    audio_file: UploadFile = File(...),
    session: DbSession = Depends(get_session),
    file_uploader: Uploader = Depends(get_file_uploader)
):
    if not audio_file.filename:
        raise exceptions.HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail="Uploaded file must have a filename",
        )

    # TODO: look into database level uuid.
    job_id = str(uuid7())
    # TODO: create an interface to use different strategies.
    input_path, input_size_bytes = file_uploader.upload(job_id, audio_file)
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
