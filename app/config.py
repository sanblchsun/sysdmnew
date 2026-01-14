# app/config.py
from typing import ClassVar
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    # === База данных ===
    DB_USER: str
    DB_PASSWORD: str
    DB_NAME: str
    DB_HOST: str

    # === Приложение ===
    APP_TITLE: str
    APP_VERSION: str
    DEBUG: bool
    APP_HOST: str
    APP_PORT: int

    # === CORS ===
    CORS_ORIGINS: str

    # === Безопасность ===
    SECRET_KEY: str
    # ALGORITHM: ClassVar[str] = "HS256"

    # === Агенты ===

    FIRST_SUPERUSER: str
    FIRST_SUPERUSER_PASSWORD: str

    # === Директории ===
    LOG_DIR: str
    UPLOAD_DIR: str

    @property
    def DATABASE_URL(self):
        return f"postgresql+asyncpg://{self.DB_USER}:{self.DB_PASSWORD}@{self.DB_HOST}:5432/{self.DB_NAME}"

    model_config = SettingsConfigDict(env_file=".env")


settings = Settings()  # type: ignore
