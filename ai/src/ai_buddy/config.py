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
    knowledge_db_dsn: str = ""
    knowledge_min_similarity: float = 0.3
    embedding_model: str = "text-embedding-3-small"
    embedding_base_url: str = ""
    embedding_api_key: str = ""
    demo_db_path: Path = Path(".data/onboarding.sqlite3")
    user_url: str = ""
    user_token: str = ""
    chat_profile_db_dsn: str = ""

    @classmethod
    def from_env(cls) -> "Settings":
        return cls(
            service_token=os.getenv("AI_SERVICE_TOKEN", ""),
            llm_api_key=os.getenv("LLM_API_KEY", ""),
            llm_model=os.getenv("LLM_MODEL", ""),
            llm_base_url=os.getenv("LLM_BASE_URL", "https://api.openai.com/v1"),
            onboarding_url=os.getenv("ONBOARDING_URL", ""),
            onboarding_token=os.getenv("ONBOARDING_SERVICE_TOKEN", ""),
            knowledge_db_dsn=os.getenv("KNOWLEDGE_DB_DSN", ""),
            knowledge_min_similarity=float(os.getenv("KNOWLEDGE_MIN_SIMILARITY", "0.3")),
            embedding_model=os.getenv("EMBEDDING_MODEL", "text-embedding-3-small"),
            embedding_base_url=os.getenv("EMBEDDING_BASE_URL", ""),
            embedding_api_key=os.getenv("EMBEDDING_API_KEY", ""),
            demo_db_path=Path(os.getenv("DEMO_DB_PATH", ".data/onboarding.sqlite3")),
            user_url=os.getenv("USER_URL", ""),
            user_token=os.getenv("USER_SERVICE_TOKEN", ""),
            chat_profile_db_dsn=os.getenv("CHAT_PROFILE_DB_DSN", ""),
        )
