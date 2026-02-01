# app/api/agent.py
from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel
from sqlalchemy.ext.asyncio import AsyncSession
import secrets
from datetime import datetime
from app.core.auth_agent import get_agent_by_token
from app.database import get_db
from app.models import Agent, AgentAdditionalData, Company
from app.schemas.agent import (
    AgentRegisterIn,
    AgentRegisterOut,
    AgentTelemetryIn,
    DiskInfoSchema,
)
from sqlalchemy import select

router = APIRouter(prefix="/api/agent", tags=["agent"])


@router.post("/register", response_model=AgentRegisterOut)
async def register_agent(
    data: AgentRegisterIn,
    session: AsyncSession = Depends(get_db),
):
    """
    Регистрация нового агента или обновление существующего.
    Принимает company_id от агента.
    """
    company_id = data.company_id

    # Проверяем, существует ли компания с таким ID
    result = await session.execute(select(Company).where(Company.id == company_id))
    company = result.scalars().first()

    if not company:
        raise HTTPException(
            status_code=404,
            detail=f"Company with ID {company_id} not found. "
            f"Make sure the company exists in the database.",
        )

    # 🔍 Ищем агента по machine_uid
    result = await session.execute(
        select(Agent).where(Agent.machine_uid == data.machine_uid)
    )
    agent = result.scalars().first()

    if agent:
        # 🔄 уже существует → обновляем
        agent.name_pc = data.name_pc
        agent.last_seen = datetime.utcnow()
    else:
        # 🆕 первый запуск → создаём
        token = secrets.token_urlsafe(32)

        agent = Agent(
            machine_uid=data.machine_uid,
            name_pc=data.name_pc,
            company_id=company_id,
            department_id=None,
            token=token,
            is_active=True,
            last_seen=datetime.utcnow(),
        )
        session.add(agent)
        await session.flush()

    # ===== additional data =====
    additional = await session.get(AgentAdditionalData, agent.id)
    if not additional:
        additional = AgentAdditionalData(agent_id=agent.id)
        session.add(additional)

    additional.system = data.system
    additional.user_name = data.user_name
    additional.ip_addr = data.ip_addr
    additional.disks = [d.model_dump() for d in data.disks]
    additional.total_memory = data.total_memory
    additional.available_memory = data.available_memory
    additional.external_ip = data.external_ip

    await session.commit()

    return AgentRegisterOut(
        agent_uuid=agent.uuid,
        token=agent.token,
    )


@router.post("/heartbeat")
async def agent_heartbeat(
    uuid: str, token: str, agent: Agent = Depends(get_agent_by_token)
):
    # agent уже проверен и last_seen обновлён
    return {"status": "ok", "agent_uuid": agent.uuid, "last_seen": agent.last_seen}


@router.post("/telemetry")
async def agent_telemetry(
    data: AgentTelemetryIn,
    agent=Depends(get_agent_by_token),
    session: AsyncSession = Depends(get_db),
):
    """
    Обновляет AgentAdditionalData агента.
    Агент аутентифицирован через uuid + token.
    """
    additional = await session.get(AgentAdditionalData, agent.id)

    # Если нет записи — создаём
    if not additional:
        additional = AgentAdditionalData(agent_id=agent.id)
        session.add(additional)

    # Обновляем системные данные
    additional.system = data.system
    additional.user_name = data.user_name
    additional.ip_addr = data.ip_addr
    additional.disks = [d.model_dump() for d in data.disks]
    additional.total_memory = data.total_memory
    additional.available_memory = data.available_memory
    additional.external_ip = data.external_ip

    # Обновляем last_seen агента
    agent.last_seen = datetime.utcnow()

    await session.commit()

    return {"status": "ok", "agent_uuid": agent.uuid}
