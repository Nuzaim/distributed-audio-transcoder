from contextlib import asynccontextmanager

from fastapi import FastAPI

from .database import db_engine
from .models.audio import Base
from .routers.audios import audio_router


@asynccontextmanager
async def lifespan(app: FastAPI):
    # WARNING: remove this. Add migrations instead.
    with db_engine.begin() as conn:
        Base.metadata.create_all(bind=conn)
    yield


app = FastAPI(lifespan=lifespan)
app.include_router(audio_router)
