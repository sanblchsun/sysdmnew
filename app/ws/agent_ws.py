# app/ws/agent_ws.py
from fastapi import APIRouter, WebSocket, WebSocketDisconnect, Depends, Query
from loguru import logger
from sqlalchemy.ext.asyncio import AsyncSession
from app.database import get_db
from app.models import Agent
from sqlalchemy import select
import json

router = APIRouter()

# Хранилище активных соединений: agent_id -> WebSocket
active_connections: dict[int, WebSocket] = {}


async def get_agent_by_token_ws(token: str, session: AsyncSession) -> Agent | None:
    result = await session.execute(select(Agent).where(Agent.token == token))
    return result.scalars().first()


@router.websocket("/ws/agent")
async def agent_websocket(
    websocket: WebSocket,
    token: str = Query(...),
    session: AsyncSession = Depends(get_db),
):
    await websocket.accept()

    agent = await get_agent_by_token_ws(token, session)
    if not agent:
        await websocket.send_json({"error": "Invalid token"})
        await websocket.close()
        return

    agent_id = agent.id
    active_connections[agent_id] = websocket
    logger.info(f"Agent {agent_id} connected via WebSocket")

    try:
        while True:
            data = await websocket.receive_text()
            # Агент может слать heartbeat или другие сообщения
            try:
                msg = json.loads(data)
                logger.debug(f"From agent {agent_id}: {msg}")
            except json.JSONDecodeError:
                pass
    except WebSocketDisconnect:
        logger.info(f"Agent {agent_id} disconnected")
    finally:
        active_connections.pop(agent_id, None)


async def send_command_to_agent(agent_id: int, command: dict) -> bool:
    """Отправить команду агенту через WebSocket"""
    ws = active_connections.get(agent_id)
    if not ws:
        return False
    try:
        await ws.send_json(command)
        return True
    except Exception as e:
        logger.error(f"Failed to send command to agent {agent_id}: {e}")
        return False
