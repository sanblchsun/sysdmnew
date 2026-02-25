# test/main.py
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from contextlib import asynccontextmanager

from test.config import settings
from test.routers import mesh
from loguru import logger
import sys


# Настройка логирования
logger.remove()
logger.add(
    sys.stdout,
    format="<green>{time:YYYY-MM-DD HH:mm:ss}</green> | <level>{level: <8}</level> | <cyan>{name}</cyan>:<cyan>{function}</cyan> - <level>{message}</level>",
    level="DEBUG" if settings.DEBUG else "INFO",
)


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Стартап
    logger.info("🚀 Запуск MeshCentral FastAPI приложения")
    logger.info(f"Режим отладки: {settings.DEBUG}")
    logger.info(f"MeshCentral сервер: {settings.MESH_SITE}")

    yield

    # Шутдаун
    logger.info("👋 Остановка приложения")


# Создание FastAPI приложения
app = FastAPI(
    title="MeshCentral API",
    description="API для управления MeshCentral",
    version="1.0.0",
    lifespan=lifespan,
)

# CORS
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Подключаем роутер
app.include_router(mesh.router)


@app.get("/")
async def root():
    return {
        "message": "MeshCentral API",
        "version": "1.0.0",
        "docs": "/docs",
        "status": "running",
    }


@app.get("/health")
async def health():
    return {"status": "healthy"}
