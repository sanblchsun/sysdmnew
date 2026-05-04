# app/main.py
import os
from pathlib import Path
from fastapi import FastAPI
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates
from fastapi import Request
from app.api import pages
from fastapi.responses import RedirectResponse, JSONResponse
from app.api import web_cookie
from app.middleware.auth_html import AuthHTMLMiddleware
from app.api.agent import router as agent_router
from app.ws import agent_ws
from fastapi.exceptions import RequestValidationError
from loguru import logger

app = FastAPI(title="SysDM RMM")
app.add_middleware(AuthHTMLMiddleware)

# Обработчик для детального логирования ошибок валидации
@app.exception_handler(RequestValidationError)
async def validation_exception_handler(request: Request, exc: RequestValidationError):
    logger.error(f"Validation error on {request.method} {request.url}")
    logger.error(f"Query params: {dict(request.query_params)}")
    logger.error(f"Validation errors: {exc.errors()}")
    return JSONResponse(
        status_code=422,
        content={"detail": exc.errors()},
    )

# Статика и шаблоны
current_dir = os.path.dirname(os.path.abspath(__file__))
static_path = os.path.join(current_dir, "static")
app.mount("/static", StaticFiles(directory=static_path), name="static")
templates = Jinja2Templates(directory="app/templates")


# API
app.include_router(web_cookie.router, tags=["web"])
app.include_router(pages.router)
app.include_router(agent_router)
app.include_router(agent_ws.router)


# Корневой путь перенаправляет на логин
@app.get("/")
async def root():
    return RedirectResponse(url="/login")
