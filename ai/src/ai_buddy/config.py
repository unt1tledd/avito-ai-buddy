import os
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Settings:
    service_token: str = ""
    llm_api_key: str = ""
    llm_model: str = ""
    llm_base_url: str = "https://api.openai.com/v1"
    onboarding_url: str = ""
    onboarding_token: str = ""
    knowledge_path: Path = Path("knowledge/documents.json")
    demo_db_path: Path = Path(".data/onboarding.sqlite3")

    @classmethod
    def from_env(cls) -> "Settings":
        return cls(
            service_token=os.getenv("AI_SERVICE_TOKEN", ""),
            llm_api_key=os.getenv("LLM_API_KEY", ""),
            llm_model=os.getenv("LLM_MODEL", ""),
            llm_base_url=os.getenv("LLM_BASE_URL", "https://api.openai.com/v1"),
            onboarding_url=os.getenv("ONBOARDING_URL", ""),
            onboarding_token=os.getenv("ONBOARDING_SERVICE_TOKEN", ""),
            knowledge_path=Path(os.getenv("KNOWLEDGE_PATH", "knowledge/documents.json")),
            demo_db_path=Path(os.getenv("DEMO_DB_PATH", ".data/onboarding.sqlite3")),
        )
