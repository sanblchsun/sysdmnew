# app/core/auth_agent.py
from datetime import datetime
from fastapi import Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select

from app.database import get_db
from app.models import Agent

async def get_agent_by_token(
    uuid: str,
    token: str,
    session: AsyncSession = Depends(get_db)
) -> Agent:
    """
    Асинхронная зависимость для аутентификации Go-агента.
    Проверяет uuid + token, возвращает объект Agent.
    """
    result = await session.execute(
        select(Agent).where(Agent.uuid == uuid, Agent.token == token)
    )
    agent = result.scalar_one_or_none()

    if not agent:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid agent credentials"
        )

    # Обновим last_seen
    agent.last_seen = datetime.utcnow()
    await session.commit()

    return agent
