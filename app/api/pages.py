# app/api/pages.py
from fastapi import APIRouter, Request
from fastapi.templating import Jinja2Templates

router = APIRouter()
templates = Jinja2Templates(directory="app/templates")


@router.get("/ui/left-menu")
async def left_menu(request: Request):
    return templates.TemplateResponse(
        "partials/left_menu.html",
        {"request": request},
    )


@router.get("/ui/top-panel")
async def top_panel(request: Request):
    return templates.TemplateResponse(
        "partials/top_panel.html",
        {"request": request},
    )


@router.get("/ui/bottom-panel")
async def bottom_panel(request: Request):
    return templates.TemplateResponse(
        "partials/bottom_panel.html",
        {"request": request},
    )
