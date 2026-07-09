from uuid import UUID
from pydantic import BaseModel
from app.models.audio import JobStatus


class QueuedAudioJob(BaseModel):
    id: UUID
    status: JobStatus
    input_path: str
    input_mime_type: str | None
    input_size_bytes: int

    model_config = {"from_attributes": True}
