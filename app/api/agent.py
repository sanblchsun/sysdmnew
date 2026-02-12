# app/api/agent.py
from pathlib import Path
from fastapi.responses import FileResponse
from loguru import logger
import os
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
from app.schemas.agent_update import (
    AgentCheckUpdateIn,
    AgentCheckUpdateOut,
)
from app.models import AgentBuild
from app.utils.hash import sha256_file
from fastapi import Body
from app.config import settings


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
        # обновляем существующего
        agent.name_pc = data.name_pc
        agent.last_seen = datetime.utcnow()
        agent.exe_version = data.exe_version  # <-- записываем версию
    else:
        # создаем нового
        token = secrets.token_urlsafe(32)
        agent = Agent(
            machine_uid=data.machine_uid,
            name_pc=data.name_pc,
            company_id=data.company_id,
            department_id=None,
            token=token,
            is_active=True,
            last_seen=datetime.utcnow(),
            exe_version=data.exe_version,  # <-- версия при регистрации
        )
        session.add(agent)
        await session.flush()

    # обновление additional_data
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
    return AgentRegisterOut(agent_uuid=agent.uuid, token=agent.token)


@router.post("/telemetry")
async def agent_telemetry(
    data: AgentTelemetryIn,
    agent=Depends(get_agent_by_token),
    session: AsyncSession = Depends(get_db),
):
    # Обновляем AgentAdditionalData
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

    # Обновляем exe_version в Agent
    if data.exe_version:
        agent.exe_version = data.exe_version

    agent.last_seen = datetime.utcnow()
    await session.commit()
    return {"status": "ok", "agent_uuid": agent.uuid}


@router.post("/heartbeat")
async def agent_heartbeat(
    uuid: str, token: str, agent: Agent = Depends(get_agent_by_token)
):
    # agent уже проверен и last_seen обновлён
    return {"status": "ok", "agent_uuid": agent.uuid, "last_seen": agent.last_seen}


@router.post("/check-update", response_model=AgentCheckUpdateOut)
async def check_update(
    data: AgentCheckUpdateIn,
    agent: Agent = Depends(get_agent_by_token),
    session: AsyncSession = Depends(get_db),
):
    logger.info(
        "Agent %s check-update. Current build: %s",
        agent.uuid,
        data.build,
    )

    # активный билд
    result = await session.execute(
        select(AgentBuild).where(AgentBuild.is_active.is_(True))
    )
    active_build = result.scalars().first()

    if not active_build:
        return AgentCheckUpdateOut(update=False)

    # если версия совпадает
    if data.build == active_build.build_slug:
        return AgentCheckUpdateOut(update=False)

    company_slug = agent.company.slug
    filename = f"agent_{company_slug}_{active_build.build_slug}.exe"
    filepath = os.path.join("builder", "dist", "agents", filename)

    if not os.path.isfile(filepath):
        logger.error("Build file not found: %s", filepath)
        return AgentCheckUpdateOut(update=False)

    return AgentCheckUpdateOut(
        update=True,
        build=active_build.build_slug,
        url=(
            f"{settings.APP_HOST}/api/agent/download"
            f"?build={active_build.build_slug}"
            f"&uuid={agent.uuid}"
            f"&token={agent.token}"
        ),
        sha256=active_build.sha256,
        force=False,
    )


@router.get("/download")
async def download_agent_build(
    build: str,
    agent: Agent = Depends(get_agent_by_token),
):
    """
    Защищённая загрузка билда.
    Доступна только агенту с валидным uuid+token.
    """

    company_slug = agent.company.slug
    filename = f"agent_{company_slug}_{build}.exe"

    base_path = Path("builder") / "dist" / "agents"
    file_path = base_path / filename

    if not file_path.exists():
        raise HTTPException(status_code=404, detail="Build file not found")

    return FileResponse(
        path=file_path,
        filename=filename,
        media_type="application/octet-stream",
    )
