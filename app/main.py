from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.models.audio import Base
from app.routers.audios import audio_router
from app.database import db_engine
from app.config import UPLOAD_DIR


@asynccontextmanager
async def lifespan(app: FastAPI):
    UPLOAD_DIR.mkdir(parents=True, exist_ok=True)
    # WARNING: remove this. Add migrations instead.
    with db_engine.begin() as conn:
        Base.metadata.create_all(bind=conn)
    yield


app = FastAPI(lifespan=lifespan)
app.include_router(audio_router)
