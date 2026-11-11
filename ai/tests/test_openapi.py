from pathlib import Path

import yaml

from ai_service.core.config import Settings
from ai_service.main import create_app


def test_checked_in_openapi_matches_app():
    spec = Path(__file__).resolve().parents[1] / "api" / "ai" / "v1" / "openapi.yaml"
    assert yaml.safe_load(spec.read_text(encoding="utf-8")) == create_app(Settings(mode="demo")).openapi()
