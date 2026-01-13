from fastapi import FastAPI
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates
from fastapi.requests import Request
from app.api import pages


app = FastAPI(title="Async 3-Panel UI")

# Статика и шаблоны
app.mount("/static", StaticFiles(directory="app/static"), name="static")
templates = Jinja2Templates(directory="app/templates")

# API
app.include_router(pages.router)


# Главная страница
@app.get("/")
async def index(request: Request):
    return templates.TemplateResponse("index.html", {"request": request})
