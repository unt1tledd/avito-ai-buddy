FROM python:3.12-slim
WORKDIR /app

RUN pip install --no-cache-dir uv==0.12.17
COPY pyproject.toml uv.lock ./
COPY src ./src
COPY knowledge ./knowledge
RUN uv sync --locked --no-dev --no-editable

EXPOSE 8000
CMD ["/app/.venv/bin/uvicorn", "ai_buddy.app:create_app", "--factory", "--host", "0.0.0.0", "--port", "8000"]