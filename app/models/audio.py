from datetime import UTC, datetime
from enum import Enum
from uuid import uuid7

from sqlalchemy import CheckConstraint, DateTime, Index, Integer, String
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


def utc_now() -> datetime:
    return datetime.now(UTC)


class Base(DeclarativeBase):
    pass


class JobStatus(str, Enum):
    QUEUED = "queued"
    PROCESSING = "processing"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELLED = "cancelled"


class TranscodeJob(Base):
    __tablename__ = "transcode_jobs"
    __table_args__ = (
        CheckConstraint("attempt_count >= 0", name="ck_transcode_jobs_attempt_count_non_negative"),
        Index("ix_transcode_jobs_queue_claim", "status", "queue_name"),
    )

    id: Mapped[str] = mapped_column(
        String(36),
        default=lambda: str(uuid7()),
        primary_key=True,
        nullable=False,
    )
    queue_name: Mapped[str] = mapped_column(String(100), default="default_queue", index=True, nullable=False)
    status: Mapped[JobStatus] = mapped_column(
        String(20),
        default=JobStatus.QUEUED,
        index=True,
        nullable=False,
    )
    attempt_count: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    input_path: Mapped[str] = mapped_column(String(1024), nullable=False)
    input_mime_type: Mapped[str | None] = mapped_column(String(255), nullable=True)
    input_size_bytes: Mapped[int | None] = mapped_column(Integer, nullable=True)
    output_path: Mapped[str | None] = mapped_column(String(1024), nullable=True)
    output_format: Mapped[str | None] = mapped_column(String(50), nullable=True)

    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utc_now, nullable=False)
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=utc_now,
        onupdate=utc_now,
        nullable=False,
    )
    started_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    completed_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    duration_seconds: Mapped[int | None] = mapped_column(Integer, nullable=True)

    def __repr__(self) -> str:
        return (
            f"<TranscodeJob(id={self.id}, "
            f"status={self.status})>"
        )
