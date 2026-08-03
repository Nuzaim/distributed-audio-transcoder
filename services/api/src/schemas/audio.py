from uuid import UUID
from pydantic import BaseModel
from ..models.audio import JobStatus


class QueuedAudioJob(BaseModel):
    id: UUID
    status: JobStatus
    input_path: str
    input_mime_type: str | None
    input_size_bytes: int
    output_path: str | None

    model_config = {"from_attributes": True}

class UpdateAudioJob(BaseModel):
    output_path: str


class AudioJobStatus(BaseModel):
    id: UUID
    status: JobStatus
    output_path: str | None
    completed: bool
