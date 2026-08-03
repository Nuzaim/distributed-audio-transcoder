import asyncio
from uuid import uuid4, UUID
from sqlalchemy import select
from sqlalchemy.orm import Session as DbSession
from fastapi import APIRouter, Depends, File, HTTPException, UploadFile, WebSocket, WebSocketDisconnect, status, exceptions

from ..config import SQS_QUEUE_URL
from ..database import get_session
from ..message_queue import MessageQueueClient, get_sqs_client
from ..models.audio import JobStatus, TranscodeJob, utc_now
from ..schemas.audio import AudioJobStatus, QueuedAudioJob, UpdateAudioJob
from ..uploader import Uploader, get_file_uploader


audio_router = APIRouter(prefix="/audio", tags=["audio"])

@audio_router.get("/{audio_id}", response_model=QueuedAudioJob, status_code=status.HTTP_200_OK)
def get_audio(
    audio_id: UUID,
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
    file_uploader: Uploader = Depends(get_file_uploader),
    mq_client: MessageQueueClient = Depends(get_sqs_client),
):
    if not audio_file.filename:
        raise exceptions.HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail="Uploaded file must have a filename",
        )

    job_id = uuid4()
    input_path, input_size_bytes = file_uploader.upload(job_id, audio_file)
    # send message about the audio to be transcoded.
    # NOTE: use message attributes here?
    mq_client.send_message(
        queue_url=SQS_QUEUE_URL,
        message_body=f'{{"input_path": "{input_path}", "job_id": "{job_id}"}}',
        message_attributes= {
            'content_type': {
                'StringValue': 'application/json',
                'DataType': 'String'
            }
        }
    )
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

# TODO: BATCH UPDATE.
@audio_router.patch("/{audio_id}", status_code=status.HTTP_204_NO_CONTENT)
def update_audio(
    audio_id: UUID,
    audio_job: UpdateAudioJob,
    session: DbSession = Depends(get_session),
):
    if not audio_job.output_path:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST)
    stmt = select(TranscodeJob).where(TranscodeJob.id==audio_id)
    job = session.execute(stmt).scalar_one_or_none()
    if not job:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Audio job with ID {audio_id} not found",
        )
    job.output_path = audio_job.output_path
    job.status = JobStatus.COMPLETED
    job.completed_at = utc_now()
    session.commit()
    session.refresh(job)
    return

@audio_router.websocket("/{audio_id}/ws")
async def stream_audio_status(
    websocket: WebSocket,
    audio_id: UUID,
):
    await websocket.accept()
    last_message: AudioJobStatus | None = None
    try:
        while True:
            session_gen = get_session()
            session = next(session_gen)
            stmt = select(TranscodeJob).where(TranscodeJob.id == audio_id)
            job = session.execute(stmt).scalar_one_or_none()
            if not job:
                await websocket.send_json({
                    "error": f"Audio job with ID {audio_id} not found",
                })
                await websocket.close(code=1008)
                return

            audio_status = AudioJobStatus(
                    id=job.id,
                    status=job.status,
                    output_path=job.output_path,
                    completed=job.status == JobStatus.COMPLETED or job.output_path is not None,
                )
            if audio_status != last_message:
                await websocket.send_json(audio_status.model_dump(mode="json"))
                last_message = audio_status
            if audio_status.completed:
                await websocket.close()
                return
            session_gen.close()
            await asyncio.sleep(1)
    except WebSocketDisconnect:
        return
