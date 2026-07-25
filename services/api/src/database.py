from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker


db_engine = create_engine("sqlite:///audio_database.sqlite", echo=True) # TODO: make this configurable via environment variable. Use dynaconf.
Session = sessionmaker(db_engine, expire_on_commit=False)

def get_session():
    with Session() as session:
        try:
            yield session
            session.commit()
        except Exception:
            session.rollback()
            raise
        finally:
            session.close()
