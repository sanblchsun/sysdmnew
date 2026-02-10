# app/api/pages.py
from typing import Optional
from datetime import datetime, timedelta
from fastapi import APIRouter, HTTPException, Request, Query, Depends
from fastapi.responses import JSONResponse
from fastapi.templating import Jinja2Templates
from loguru import logger
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import and_, select
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
        "exe_version",
    ]

    if not target_id or not target_type:
        return templates.TemplateResponse(
            "partials/top_panel.html",
            {
                "request": request,
                "agents": [],
                "agent_columns": columns,
            },
        )

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
    return templates.TemplateResponse(
        "partials/bottom_panel.html",
        {"request": request, "agent": agent},
    )


@router.get("/api/agents/status")
async def agents_status(
    target_id: int = Query(...),
    target_type: str = Query(...),
    session: AsyncSession = Depends(get_db),
):
    conditions = []

    if target_type == "company":
        conditions.append(Agent.company_id == target_id)

    elif target_type == "department":
        conditions.append(Agent.department_id == target_id)

    elif target_type == "unassigned":
        conditions.append(
            and_(
                Agent.company_id == target_id,
                Agent.department_id.is_(None),
            )
        )
    else:
        raise HTTPException(status_code=400, detail="Invalid target_type")

    stmt = select(Agent.id, Agent.last_seen).where(*conditions)

    now = datetime.utcnow()
    result = await session.execute(stmt)

    agents = {}
    for agent_id, last_seen in result.all():
        agents[agent_id] = {
            "online": bool(last_seen and (now - last_seen) < OFFLINE_AFTER)
        }

    return {
        "ts": now.isoformat(),
        "agents": agents,
    }


# модальное окно
# app/api/pages.py - добавляем новые функции
@router.get("/ui/modal-panel")
async def modal_panel(
    request: Request, agent_id: int, session: AsyncSession = Depends(get_db)
):
    """Возвращает HTML для модального окна изменения отдела агента"""

    # Получаем данные агента
    stmt_agent = (
        select(
            Agent.id,
            Agent.name_pc,
            Agent.company_id,
            Agent.department_id,
            AgentAdditionalData.user_name,
            Company.name.label("company_name"),
            Department.name.label("department_name"),
        )
        .outerjoin(AgentAdditionalData, AgentAdditionalData.agent_id == Agent.id)
        .outerjoin(Company, Agent.company_id == Company.id)
        .outerjoin(Department, Agent.department_id == Department.id)
        .where(Agent.id == agent_id)
    )

    result = await session.execute(stmt_agent)
    row = result.first()

    if not row:
        return templates.TemplateResponse("partials/empty.html", {"request": request})

    # Получаем все отделы этой компании
    stmt_departments = (
        select(Department.id, Department.name)
        .where(Department.company_id == row.company_id)
        .order_by(Department.name)
    )

    dept_result = await session.execute(stmt_departments)
    departments = dept_result.all()

    agent_data = {
        "id": row.id,
        "name_pc": row.name_pc,
        "user_name": row.user_name or "Неизвестно",
        "company_name": row.company_name or "Неизвестно",
        "current_department_id": row.department_id,  # Может быть None
        "current_department_name": row.department_name or "Без отдела",
        "company_id": row.company_id,
    }

    return templates.TemplateResponse(
        "partials/agent_modal.html",
        {"request": request, "agent": agent_data, "departments": departments},
    )


# app/api/pages.py - обновленная функция change_agent_department
@router.post("/api/agent/{agent_id}/change-department")
async def change_agent_department(
    agent_id: int, request: Request, session: AsyncSession = Depends(get_db)
):
    """Изменение отдела агента с возвратом данных для обновления UI"""
    try:
        data = await request.json()
        department_id = data.get("department_id")

        # Получаем агента
        agent = await session.get(Agent, agent_id)
        if not agent:
            return JSONResponse(
                status_code=404,
                content={
                    "status": "error",
                    "message": f"Агент с ID {agent_id} не найден",
                },
            )

        # Запоминаем старый отдел для логики
        old_department_id = agent.department_id

        # Если department_id = 0 или None, убираем отдел
        if not department_id or department_id == 0:
            agent.department_id = None
            new_department_name = "Без отдела"
            message = f"Агент {agent.name_pc} перемещен в 'Без отдела'"
        else:
            # Проверяем существование отдела
            department = await session.get(Department, department_id)
            if not department:
                return JSONResponse(
                    status_code=404,
                    content={
                        "status": "error",
                        "message": f"Отдел с ID {department_id} не найден",
                    },
                )

            agent.department_id = department_id
            new_department_name = department.name
            message = f"Агент {agent.name_pc} перемещен в отдел '{department.name}'"

        await session.commit()

        # Получаем обновленные данные для ответа
        from app.repositories.tree import get_tree

        return {
            "status": "success",
            "message": message,
            "agent_id": agent_id,
            "new_department_id": agent.department_id,
        }
    except Exception as e:
        await session.rollback()
        logger.error(f"Ошибка изменения отдела агента {agent_id}: {e}")
        return JSONResponse(
            status_code=500,
            content={
                "status": "error",
                "message": f"Ошибка при изменении отдела: {str(e)}",
            },
        )
