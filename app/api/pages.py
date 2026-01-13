# app/api/pages.py
from fastapi import APIRouter, Request
from fastapi.templating import Jinja2Templates
from app.repositories.tree import get_tree
from sqlalchemy.ext.asyncio import AsyncSession
from fastapi import APIRouter, Request, Depends
from app.database import get_db

router = APIRouter()
templates = Jinja2Templates(directory="app/templates")


@router.get("/ui/left-menu")
async def tree(request: Request, session: AsyncSession = Depends(get_db)):
    companies = await get_tree(session)
    return templates.TemplateResponse(
        "partials/left_menu.html", {"request": request, "companies": companies}
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
