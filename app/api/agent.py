# app/api/agent.py
from pathlib import Path
from fastapi.responses import FileResponse
from loguru import logger
import os
from fastapi import APIRouter, Depends, Form, HTTPException, Request
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

# -------------------- TOP PANEL --------------------
router = APIRouter(prefix="/api/agent", tags=["agent"])


@router.post("/register", response_model=AgentRegisterOut)
async def register_agent(
    request: Request,
    data: AgentRegisterIn,
    session: AsyncSession = Depends(get_db),
):
    """
    Регистрация нового агента или обновление существующего.
    Автоматическое определение компании по external_ip.
    """

    # -------------------------
    # 1. Определяем IP агента
    # -------------------------
    forwarded = request.headers.get("x-forwarded-for")
    if forwarded:
        client_ip = forwarded.split(",")[0].strip()
    elif request.client:
        client_ip = request.client.host
    else:
        client_ip = None

    # если внешнее поле передали, используем его
    if data.external_ip:
        client_ip = data.external_ip

    if not client_ip:
        raise HTTPException(status_code=400, detail="Cannot determine client IP")

    # -------------------------
    # 2. Определяем компанию
    # -------------------------
    company_id = data.company_id

    if not company_id:
        # ищем компанию по external_ip
        result = await session.execute(
            select(Company).where(Company.external_ip == client_ip)
        )
        company = result.scalars().first()
        if company:
            company_id = company.id
        else:
            raise HTTPException(
                status_code=400,
                detail=f"Cannot determine company for agent with IP {client_ip}. Pass company_id explicitly.",
            )
    else:
        # проверяем, что компания существует
        company = await session.get(Company, company_id)
        if not company:
            raise HTTPException(
                status_code=404, detail=f"Company with id {company_id} not found"
            )

    # -------------------------
    # 3. Ищем агента по machine_uid
    # -------------------------
    result = await session.execute(
        select(Agent).where(Agent.machine_uid == data.machine_uid)
    )
    agent = result.scalars().first()

    now = datetime.utcnow()

    if agent:
        # обновляем существующего
        agent.name_pc = data.name_pc
        agent.last_seen = now
        agent.company_id = company_id
        agent.exe_version = data.exe_version
    else:
        # создаём нового
        token = secrets.token_urlsafe(32)
        agent = Agent(
            machine_uid=data.machine_uid,
            name_pc=data.name_pc,
            company_id=company_id,
            department_id=None,
            token=token,
            is_active=True,
            last_seen=now,
            exe_version=data.exe_version,
        )
        session.add(agent)
        await session.flush()

    # -------------------------
    # 4. Обновляем additional_data
    # -------------------------
    additional = await session.get(AgentAdditionalData, agent.id)
    if not additional:
        additional = AgentAdditionalData(agent_id=agent.id)
        session.add(additional)

    additional.system = data.system
    additional.user_name = data.user_name
    additional.ip_addr = client_ip
    additional.disks = [d.model_dump() for d in data.disks]
    additional.total_memory = data.total_memory
    additional.available_memory = data.available_memory
    additional.external_ip = client_ip

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
    filename = f"agent_universal_{active_build.build_slug}.exe"
    filepath = os.path.join("builder", "dist", "agents", filename)

    if not os.path.isfile(filepath):
        logger.error(f"Build file not found (check_update): {filepath}")
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

    filename = f"agent_universal_{build}.exe"

    base_path = Path("builder") / "dist" / "agents"
    file_path = base_path / filename

    if not file_path.exists():
        logger.error(f"Build file not found (download): {file_path}")
        raise HTTPException(status_code=404, detail="Build file not found")

    return FileResponse(
        path=file_path,
        filename=filename,
        media_type="application/octet-stream",
    )


@router.post("/company/{company_id}/set-external-ip")
async def set_external_ip(
    company_id: int,
    external_ip: str = Form(...),
    session: AsyncSession = Depends(get_db),
):
    company = await session.get(Company, company_id)
    if not company:
        raise HTTPException(status_code=404, detail="Company not found")

    company.external_ip = external_ip
    await session.commit()

    return {"status": "ok", "external_ip": company.external_ip}
