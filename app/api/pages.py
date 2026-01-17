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
from sqlalchemy.orm import joinedload

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
    columns = [" ", "Имя ПК", "Имя сотрудника", "IP", "Компания", "Отдел"]

    if target_id is not None and target_type is not None:
        # Объединяем общие части запроса
        common_query = (
            select(
                Agent.id,  # Обязательно включаем id
                Agent.system,
                Agent.name_pc,
                Agent.user_name,
                Agent.ip_addr,
                Company.name.label("company_name"),
                Department.name.label("department_name"),
            )
            .join(Agent.department)
            .join(Department.company)
        )

        # Добавляем условия в зависимости от типа запроса
        if target_type == "company":
            stmt = common_query.where(Company.id == target_id)
        elif target_type == "department":
            stmt = common_query.where(Department.id == target_id)
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


@router.get("/ui/agent-details/{agent_id}")
async def agent_details(
    agent_id: int, request: Request, session: AsyncSession = Depends(get_db)
):
    # Получаем агента по ID
    agent_stmt = (
        select(Agent)
        .options(joinedload(Agent.additional_data))
        .where(Agent.id == agent_id)
    )
    agent_result = await session.execute(agent_stmt)
    agent = agent_result.scalars().first()

    if not agent:
        raise HTTPException(status_code=404, detail="Agent not found")

    # Используем существующий шаблон
    return templates.TemplateResponse(
        "partials/bottom_panel.html",
        {
            "request": request,
            "agent": agent,
            "additional_data": agent.additional_data or {},
        },
    )
