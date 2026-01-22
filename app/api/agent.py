# app/api/agent.py
from fastapi import APIRouter, Depends
from pydantic import BaseModel
from sqlalchemy.ext.asyncio import AsyncSession
import secrets
from datetime import datetime
from app.core.auth_agent import get_agent_by_token
from app.database import get_db
from app.models import Agent, AgentAdditionalData
from app.schemas.agent import AgentRegisterIn, AgentRegisterOut, DiskInfoSchema

router = APIRouter(prefix="/api/agent", tags=["agent"])


@router.post("/register", response_model=AgentRegisterOut)
async def register_agent(
    data: AgentRegisterIn,
    session: AsyncSession = Depends(get_db),
):
    # ⚠️ временно хардкод
    COMPANY_ID = 1

    token = secrets.token_urlsafe(32)

    agent = Agent(
        name_pc=data.name_pc,
        company_id=COMPANY_ID,
        department_id=None,
        token=token,
        is_active=True,
        last_seen=datetime.utcnow(),
    )

    session.add(agent)
    await session.flush()

    additional = AgentAdditionalData(
        agent_id=agent.id,
        system=data.system,
        user_name=data.user_name,
        ip_addr=data.ip_addr,
        disks=[d.model_dump() for d in data.disks],
        total_memory=data.total_memory,
        available_memory=data.available_memory,
        external_ip=data.external_ip,
    )

    session.add(additional)
    await session.commit()

    return AgentRegisterOut(
        agent_uuid=agent.uuid,
        token=token,
    )


@router.post("/heartbeat")
async def agent_heartbeat(
    uuid: str, token: str, agent: Agent = Depends(get_agent_by_token)
):
    # agent уже проверен и last_seen обновлён
    return {"status": "ok", "agent_uuid": agent.uuid, "last_seen": agent.last_seen}


class AgentTelemetryIn(BaseModel):
    system: str | None = None
    user_name: str | None = None
    ip_addr: str | None = None
    disks: list[DiskInfoSchema] = []
    total_memory: int | None = None
    available_memory: int | None = None
    external_ip: str | None = None


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
