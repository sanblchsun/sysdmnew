# app/api/pages.py
from typing import Literal, Optional
from fastapi import APIRouter, HTTPException, Request
from fastapi.templating import Jinja2Templates
from loguru import logger
from app.repositories.tree import get_tree
from sqlalchemy.ext.asyncio import AsyncSession
from fastapi import APIRouter, Request, Depends
from app.database import get_db
from fastapi import Query
from sqlalchemy import select
from sqlalchemy.inspection import inspect
from app.models import Agent, Company, Department
from typing import Union

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
    target_id: Optional[int] = Query(None),
    target_type: Optional[str] = Query(None),
    session: AsyncSession = Depends(get_db),
):
    agents = []
    columns = ["Имя ПК", "Имя сотрудника", "IP", "Компания", "Отдел"]

    if target_id is not None and target_type is not None:
        if target_type == "company":
            stmt = (
                select(
                    Agent.name_pc,
                    Agent.user_name,
                    Agent.ip_addr,
                    Company.name.label("company_name"),
                    Department.name.label("department_name"),
                )  # Изменяем запрос
                .join(Agent.department)
                .join(Department.company)
                .where(Company.id == target_id)
            )
        elif target_type == "department":
            stmt = (
                select(
                    Agent.name_pc,
                    Agent.user_name,
                    Agent.ip_addr,
                    Company.name.label("company_name"),
                    Department.name.label("department_name"),
                )  # Изменяем запрос
                .join(Agent.department)
                .join(Department.company)
                .where(Department.id == target_id)
            )
        else:
            raise HTTPException(status_code=400, detail="Invalid target type.")

        result = await session.execute(stmt)
        agents = result.all()  # Берём полный результат
        logger.debug(f"{agents}")

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
