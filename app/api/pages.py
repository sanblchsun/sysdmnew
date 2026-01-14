# app/api/pages.py
from fastapi import APIRouter, Request
from fastapi.templating import Jinja2Templates
from app.repositories.tree import get_tree
from sqlalchemy.ext.asyncio import AsyncSession
from fastapi import APIRouter, Request, Depends
from app.database import get_db
from fastapi import Query
from sqlalchemy import select
from sqlalchemy.inspection import inspect
from app.models import Agent, Department

router = APIRouter()
templates = Jinja2Templates(directory="app/templates")


@router.get("/ui/left-menu")
async def tree(request: Request, session: AsyncSession = Depends(get_db)):
    companies = await get_tree(session)
    return templates.TemplateResponse(
        "partials/left_menu.html", {"request": request, "companies": companies}
    )


@router.get("/ui/top-panel")
async def top_panel(
    request: Request,
    company_id: int | None = Query(None),
    session: AsyncSession = Depends(get_db),
):
    agents = []
    columns = []

    if company_id is not None:
        stmt = (
            select(Agent)
            .join(Agent.department)
            .where(Department.company_id == company_id)
        )

        result = await session.execute(stmt)
        agents = result.scalars().all()

        # Динамическое получение имен столбцов модели Агент
        mapper = inspect(Agent)
        columns = [col.key for col in mapper.columns]

    return templates.TemplateResponse(
        "partials/top_panel.html",
        {
            "request": request,
            "agents": agents,
            "agent_columns": columns,
        },
    )


@router.get("/ui/bottom-panel")
async def bottom_panel(request: Request):
    return templates.TemplateResponse(
        "partials/bottom_panel.html",
        {"request": request},
    )
