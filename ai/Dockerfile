FROM python:3.12-slim AS build
COPY --from=ghcr.io/astral-sh/uv:0.12.17 /uv /usr/local/bin/uv
WORKDIR /app
ENV UV_PYTHON_DOWNLOADS=never
COPY pyproject.toml uv.lock ./
RUN uv sync --locked --no-dev --no-install-project
COPY src ./src
RUN uv sync --locked --no-dev --no-editable

FROM python:3.12-slim
WORKDIR /app
COPY --from=build /app/.venv ./.venv
COPY migrations ./migrations
COPY knowledge ./knowledge
RUN mkdir .data && chown 65532:65532 .data
USER 65532:65532
ENV UVICORN_PORT=8080
EXPOSE 8080
CMD ["/app/.venv/bin/uvicorn", "ai_service.main:create_app", "--factory", "--host", "0.0.0.0", "--no-access-log"]
