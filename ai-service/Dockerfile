FROM python:3.11-slim

# Ensure Python stdout/stderr are not buffered for better log visibility
ENV PYTHONUNBUFFERED=1

WORKDIR /app

# Install curl (needed by HEALTHCHECK) and clean up apt cache in one layer
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*

# Install Python dependencies first (layer caching)
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

EXPOSE 8000

HEALTHCHECK --interval=10s --timeout=5s --retries=5 \
  CMD curl -sf http://localhost:8000/health || exit 1

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]
