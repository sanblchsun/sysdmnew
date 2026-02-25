# test/config.py
from pydantic_settings import BaseSettings, SettingsConfigDict
from pydantic import Field, validator
from typing import Optional
import re
import base64
from loguru import logger


class Settings(BaseSettings):
    # MeshCentral настройки
    MESH_USERNAME: str = Field(
        "~t:gMnbyvOEAAu9oCjp",
        description="Имя пользователя MeshCentral (с префиксом ~t:)",
    )
    MESH_TOKEN_KEY: str = Field(
        "2lgJDIOgtSvaE5p0Bao9", description="Токен для аутентификации"
    )
    MESH_SITE: str = Field(
        "wss://localhost:4443", description="URL MeshCentral сервера"
    )

    # Настройки приложения
    DEBUG: bool = Field(False, description="Режим отладки")

    @validator("MESH_TOKEN_KEY")
    def validate_token(cls, v):
        """Преобразуем токен в hex-формат если нужно"""
        if not v:
            raise ValueError("MESH_TOKEN_KEY не может быть пустым")

        v = v.strip()

        # Проверяем, может это уже hex?
        if re.match(r"^[0-9a-fA-F]+$", v):
            logger.info("Токен уже в hex-формате")
            return v

        # Пробуем декодировать из base64
        try:
            padded = v
            padding = 4 - (len(v) % 4)
            if padding != 4:
                padded = v + "=" * padding

            decoded = base64.b64decode(padded)
            hex_token = decoded.hex()
            logger.info(f"Токен преобразован из base64 в hex")
            return hex_token
        except Exception as e:
            logger.error(f"Не удалось преобразовать токен из base64: {e}")
            raise ValueError(
                f"MESH_TOKEN_KEY должен быть в hex или base64 формате. Получено: {v[:20]}..."
            )

    # ❌ УДАЛЯЕМ этот валидатор - он портит username
    # @validator("MESH_USERNAME")
    # def validate_username(cls, v):
    #     if v.startswith("~t:"):
    #         username = v[3:]
    #         logger.info(f"Извлечено имя пользователя из ~t: префикса: {username}")
    #         return username
    #     return v

    model_config = SettingsConfigDict(
        env_file=".env", env_file_encoding="utf-8", extra="ignore"
    )


settings = Settings()  # type: ignore
