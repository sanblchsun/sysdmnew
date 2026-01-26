# app/api/pages.py
from typing import Optional
from datetime import datetime, timedelta
from fastapi import APIRouter, HTTPException, Request, Query, Depends
from fastapi.templating import Jinja2Templates
from loguru import logger
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select
from sqlalchemy.orm import joinedload
from app.database import get_db
from app.models import Agent, AgentAdditionalData, Company, Department
from app.repositories.tree import get_tree

router = APIRouter()
templates = Jinja2Templates(directory="app/templates")


OFFLINE_AFTER = timedelta(minutes=1)


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
    columns = [
        "---",
        "Имя ПК",
        "Имя сотрудника",
        "IP",
        "Компания",
        "Отдел",
        "Online",
    ]

    if target_id is not None and target_type is not None:
        target_id = int(target_id)

        stmt = (
            select(
                Agent.id,
                Agent.name_pc,
                Agent.last_seen,  # серверное время последнего heartbeat
                AgentAdditionalData.system,
                AgentAdditionalData.user_name,
                AgentAdditionalData.ip_addr,
                Company.name.label("company_name"),
                Department.name.label("department_name"),
            )
            .outerjoin(AgentAdditionalData, AgentAdditionalData.agent_id == Agent.id)
            .outerjoin(Department, Agent.department_id == Department.id)
            .outerjoin(Company, Agent.company_id == Company.id)
        )

        if target_type == "company":
            stmt = stmt.where(Company.id == target_id)
        elif target_type == "department":
            stmt = stmt.where(Department.id == target_id)
        elif target_type == "unassigned":
            stmt = stmt.where(
                Company.id == target_id,
                Agent.department_id.is_(None),
            )
        else:
            raise HTTPException(status_code=400, detail="Invalid target_type")

        result = await session.execute(stmt)

        now = datetime.utcnow()
        for row in result.all():
            # Онлайн считается только на основе серверного last_seen
            is_online = (
                row.last_seen is not None and (now - row.last_seen) < OFFLINE_AFTER
            )

            agents.append(
                {
                    "id": row.id,
                    "name_pc": row.name_pc,
                    "system": row.system,
                    "user_name": row.user_name,
                    "ip_addr": row.ip_addr,
                    "company_name": row.company_name,
                    "department_name": row.department_name,
                    "is_online": is_online,
                }
            )

    return templates.TemplateResponse(
        "partials/top_panel.html",
        {
            "request": request,
            "agents": agents,
            "agent_columns": columns,
        },
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
    logger.debug(agent)
    return templates.TemplateResponse(
        "partials/bottom_panel.html",
        {"request": request, "agent": agent},
    )
