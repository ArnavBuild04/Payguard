"""All tunable settings in one place, read from environment variables (see .env.example).

GEMINI_API_KEY is intentionally optional: leaving it unset makes the agent fall back to a
deterministic fake LLM and fake embeddings, so the whole service runs and is testable with
zero external calls and zero cost. Set it to switch to the real Gemini model.
"""

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    gemini_api_key: str = ""
    gemini_model: str = "gemini-flash-latest"
    gemini_embedding_model: str = "text-embedding-004"

    core_base_url: str = "http://localhost:8080"

    db_host: str = "localhost"
    db_port: int = 5432
    db_user: str = "payguard"
    db_password: str = "payguard"
    db_name: str = "payguard"

    embedding_dim: int = 768
    rag_top_k: int = 3

    http_port: int = 8090

    @property
    def database_dsn(self) -> str:
        return (
            f"host={self.db_host} port={self.db_port} "
            f"user={self.db_user} password={self.db_password} dbname={self.db_name}"
        )

    @property
    def use_real_llm(self) -> bool:
        return bool(self.gemini_api_key)


settings = Settings()
