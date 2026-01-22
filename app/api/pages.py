# app/api/pages.py
from typing import Optional
from fastapi import APIRouter, HTTPException, Request, Query, Depends
from fastapi.templating import Jinja2Templates
from loguru import logger
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select
from sqlalchemy.orm import joinedload
from app.database import get_db
from app.models import Agent, Company, Department
from app.repositories.tree import get_tree

router = APIRouter()
templates = Jinja2Templates(directory="app/templates")


# -------------------- LEFT MENU --------------------
@router.get("/ui/left-menu")
async def tree(request: Request, session: AsyncSession = Depends(get_db)):
    companies = await get_tree(session)
    return templates.TemplateResponse(
        "partials/left_menu.html",
        {"request": request, "companies": companies},
    )


# -------------------- TOP PANEL --------------------
@router.get("/ui/top-panel")
async def top_panel(
    request: Request,
    target_id: Optional[int] = Query(None),
    target_type: Optional[str] = Query(None),
    session: AsyncSession = Depends(get_db),
):
    agents = []
    columns = [" ", "Имя ПК", "Система", "Имя сотрудника", "IP", "Компания", "Отдел"]

    if target_id is not None and target_type is not None:
        target_type = target_type.strip()
        try:
            target_id = int(str(target_id).strip())
        except ValueError:
            raise HTTPException(status_code=400, detail="Invalid target_id")

        from app.models import AgentAdditionalData

        common_query = (
            select(
                Agent.id,
                Agent.name_pc,
                AgentAdditionalData.system,
                AgentAdditionalData.user_name,
                AgentAdditionalData.ip_addr,
                Company.name.label("company_name"),
                Department.name.label("department_name"),
            )
            .join(Agent.department)
            .join(Department.company)
            .outerjoin(Agent.additional_data)
        )

        if target_type == "company":
            stmt = common_query.where(Company.id == target_id)
        elif target_type == "department":
            stmt = common_query.where(Department.id == target_id)
        else:
            raise HTTPException(status_code=400, detail="Invalid target_type")

        result = await session.execute(stmt)
        agents = result.all()

    return templates.TemplateResponse(
        "partials/top_panel.html",
        {"request": request, "agents": agents, "agent_columns": columns},
    )


# -------------------- AGENT DETAILS --------------------
@router.get("/ui/bottom-panel")
async def bottom_panel(
    request: Request,
    agent_id: int | None = None,
    session: AsyncSession = Depends(get_db),
):
    agent = None
    if agent_id:
        stmt = (
            select(Agent)
            .options(joinedload(Agent.additional_data))
            .where(Agent.id == agent_id)
        )
        result = await session.execute(stmt)
        agent = result.scalars().first()

    return templates.TemplateResponse(
        "partials/bottom_panel.html",
        {"request": request, "agent": agent},
    )
