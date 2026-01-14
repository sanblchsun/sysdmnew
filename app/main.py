import os
from fastapi import FastAPI
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates
from fastapi.requests import Request
from app.api import pages
from fastapi.responses import RedirectResponse
from app.api import web_cookie
from app.middleware.auth_html import AuthHTMLMiddleware


app = FastAPI(title="SysDM RMM")
app.add_middleware(AuthHTMLMiddleware)

# Статика и шаблоны
current_dir = os.path.dirname(os.path.abspath(__file__))
static_path = os.path.join(current_dir, "static")
app.mount("/static", StaticFiles(directory=static_path), name="static")
templates = Jinja2Templates(directory="app/templates")

# API
app.include_router(web_cookie.router, tags=["web"])
app.include_router(pages.router)


# Корневой путь перенаправляет на логин
@app.get("/")
async def root():
    return RedirectResponse(url="/login")
