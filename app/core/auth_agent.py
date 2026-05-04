# app/core/auth_agent.py
from fastapi import Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select
from sqlalchemy.orm import selectinload
from loguru import logger

from app.database import get_db
from app.models import Agent


async def get_agent_by_token(
    uuid: str,
    token: str,
    session: AsyncSession = Depends(get_db),
) -> Agent:
    
    logger.debug(f"Auth attempt - UUID: {uuid}, Token: {token[:20]}...")

    result = await session.execute(
        select(Agent)
        .options(selectinload(Agent.company))
        .where(
            Agent.uuid == uuid,
            Agent.token == token,
            Agent.is_active.is_(True),  # 🔐 дополнительно проверяем активность
        )
    )

    agent = result.scalar_one_or_none()

    if not agent:
        logger.warning(f"Auth FAILED - UUID: {uuid}")
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid agent credentials",
        )

    logger.debug(f"Auth SUCCESS - Agent UUID: {agent.uuid}")
    return agent
