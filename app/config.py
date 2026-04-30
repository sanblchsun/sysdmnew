# app/config.py
from typing import ClassVar
from pydantic_settings import BaseSettings, SettingsConfigDict
from pydantic import validator


class Settings(BaseSettings):
    # === База данных ===
    DB_USER: str
    DB_PASSWORD: str

    @validator("DB_PASSWORD")
    def db_password_must_not_be_empty(cls, v):
        if not v:
            raise ValueError("DB_PASSWORD cannot be empty")
        return v

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

    @validator("SECRET_KEY")
    def secret_key_must_not_be_empty(cls, v):
        if not v or len(v) < 32:
            raise ValueError("SECRET_KEY must be at least 32 characters long")
        return v

    # === Агенты ===
    FIRST_SUPERUSER: str
    FIRST_SUPERUSER_PASSWORD: str
    DISABLE_IP_FILTER: bool = False

    # === Директории ===
    LOG_DIR: str
    UPLOAD_DIR: str

    @property
    def DATABASE_URL(self):
        return f"postgresql+asyncpg://{self.DB_USER}:{self.DB_PASSWORD}@{self.DB_HOST}:5432/{self.DB_NAME}"

    model_config = SettingsConfigDict(env_file=".env")


settings = Settings()  # type: ignore
