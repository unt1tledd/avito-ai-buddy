FROM python:3.12-slim
COPY --from=ghcr.io/astral-sh/uv:0.12.17 /uv /bin/uv
WORKDIR /app
COPY pyproject.toml uv.lock ./
COPY src ./src
RUN uv sync --locked --no-dev --no-editable
COPY knowledge ./knowledge
EXPOSE 8000
CMD ["/app/.venv/bin/uvicorn", "ai_buddy.app:create_app", "--factory", "--host", "0.0.0.0"]
