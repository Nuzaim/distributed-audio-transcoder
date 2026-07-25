# --- Build Stage ---
FROM python:3.14-slim AS builder

WORKDIR /code/services/api

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1

# Create a virtual environment to isolate our dependencies
RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

# Install dependencies inside the virtual environment
RUN pip install fastapi[standard] SQLAlchemy boto3


# --- Final Production Stage ---
FROM python:3.14-slim AS runner

WORKDIR /code/services/api

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
# Make sure the final stage uses the virtual environment's binaries
ENV PATH="/opt/venv/bin:$PATH"

# Copy the entire virtual environment from the builder
COPY --from=builder /opt/venv /opt/venv
# Copy your application code
COPY services/api/src /code/services/api/src

EXPOSE 8000

# Run Uvicorn using the venv path
CMD ["uvicorn", "src.main:app", "--host", "0.0.0.0", "--port", "8000", "--workers", "4"]
