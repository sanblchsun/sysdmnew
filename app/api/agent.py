# app/api/agent.py
from pathlib import Path
from fastapi.responses import FileResponse
from loguru import logger
import os
from fastapi import APIRouter, Depends, Form, HTTPException, Request
from pydantic import BaseModel
from sqlalchemy.ext.asyncio import AsyncSession
import secrets
from datetime import datetime, timedelta
from app.core.auth_agent import get_agent_by_token
from app.database import get_db
from app.models import Agent, AgentAdditionalData, Company
from app.schemas.agent import (
    AgentRegisterIn,
    AgentRegisterOut,
    AgentTelemetryIn,
)
from sqlalchemy import select, update
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
UPDATE_INTERVAL = timedelta(seconds=60)


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
    if not settings.DISABLE_IP_FILTER:
        # Режим интернета: используем external_ip агента для поиска компании
        client_ip = data.external_ip
        if not client_ip:
            # Если агент не передал external_ip, берём IP подключения
            if request.client:
                client_ip = request.client.host
            forwarded = request.headers.get("x-forwarded-for")
            if forwarded:
                client_ip = forwarded.split(",")[0].strip()
    else:
        # Режим локальной сети: фильтр отключен, IP не используется для поиска компании
        client_ip = None

    agent_external_ip = data.external_ip

    # -------------------------
    # 2. Определяем компанию
    # -------------------------
    company_id = data.company_id

    if not company_id:
        if not settings.DISABLE_IP_FILTER:
            # Интернет-режим: ищем компанию по external_ip
            if not client_ip:
                logger.warning("Cannot determine client IP for agent registration")
                raise HTTPException(status_code=400, detail="Cannot determine client IP")
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
            # Локальная сеть: берём первую компанию как дефолтную
            result = await session.execute(select(Company).limit(1))
            company = result.scalars().first()
            if not company:
                raise HTTPException(
                    status_code=400,
                    detail="No companies found. Create a company or pass company_id explicitly.",
                )
            company_id = company.id
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
    logger.info(f"Received telemetry from agent {agent.uuid} (mode: {agent.telemetry_mode})")
    
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
    uuid: str,
    token: str,
    agent: Agent = Depends(get_agent_by_token),
    session: AsyncSession = Depends(get_db),
):
    now = datetime.utcnow()

    # Обновляем last_seen не чаще чем раз в UPDATE_INTERVAL секунд
    if not agent.last_seen or now - agent.last_seen > UPDATE_INTERVAL:
        await session.execute(
            update(Agent).where(Agent.id == agent.id).values(last_seen=now)
        )
        await session.commit()

        return {
            "status": "ok",
            "agent_uuid": agent.uuid,
            "last_seen": now,
        }

    # Если обновление не требуется — не трогаем БД
    logger.info(f"Heartbeat from agent {agent.uuid}, sending telemetry_mode: {agent.telemetry_mode}")
    return {
        "status": "ok",
        "agent_uuid": agent.uuid,
        "last_seen": agent.last_seen,
        "telemetry_mode": agent.telemetry_mode,  # Tell agent what to send
    }


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


# -------------------- TELEMETRY MODE --------------------
from app.schemas.agent import AgentTelemetryModeUpdate


@router.post("/{agent_id}/telemetry-mode")
async def set_telemetry_mode(
    agent_id: int,
    telemetry_mode: str = "none",
    session: AsyncSession = Depends(get_db),
):
    """Установить режим телеметрии для агента (none, basic, full)."""
    if telemetry_mode not in ["none", "basic", "full"]:
        raise HTTPException(status_code=400, detail="Invalid telemetry_mode. Use: none, basic, full")

    agent = await session.get(Agent, agent_id)
    if not agent:
        raise HTTPException(status_code=404, detail="Agent not found")

    old_mode = agent.telemetry_mode
    agent.telemetry_mode = telemetry_mode
    await session.commit()
    
    logger.info(f"Agent {agent.uuid} telemetry mode changed: {old_mode} -> {telemetry_mode}")

    return {"status": "ok", "telemetry_mode": agent.telemetry_mode}
