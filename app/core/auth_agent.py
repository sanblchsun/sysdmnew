# app/core/auth_agent.py
from datetime import datetime
from fastapi import Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select
from sqlalchemy.orm import selectinload

from app.database import get_db
from app.models import Agent


async def get_agent_by_token(
    uuid: str, token: str, session: AsyncSession = Depends(get_db)
) -> Agent:

    result = await session.execute(
        select(Agent)
        .options(selectinload(Agent.company))
        .where(Agent.uuid == uuid, Agent.token == token)
    )
    agent = result.scalar_one_or_none()

    if not agent:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED, detail="Invalid agent credentials"
        )

    agent.last_seen = datetime.utcnow()
    await session.commit()

    return agent
