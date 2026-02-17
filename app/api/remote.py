# app/api/remote.py
from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession

from app.models import Agent
from app.database import get_db
from app.core.auth_agent import get_agent_by_token
from app.config import settings

import httpx

router = APIRouter(prefix="/remote", tags=["remote"])


async def meshcentral_create_token(agent_uuid: str) -> str:
    """
    Создаёт временный токен для удалённого рабочего стола агента через MeshCentral API.
    """
    base_url = settings.MESH_URL
    api_key = settings.MESH_API_KEY

    async with httpx.AsyncClient(verify=False, timeout=5.0) as client:
        resp = await client.post(
            f"{base_url}/api/tokens",
            headers={"Authorization": f"Bearer {api_key}"},
            json={
                "account": agent_uuid,
                "permissions": ["remotecontrol"],
                "expire": 300  # 5 минут
            }
        )
        if resp.status_code != 200:
            raise HTTPException(
                status_code=status.HTTP_502_BAD_GATEWAY,
                detail=f"MeshCentral API error: {resp.text}"
            )
        data = resp.json()
        return data["token"]


@router.post("/{agent_uuid}/link")
async def get_remote_desktop_link(
    agent: Agent = Depends(get_agent_by_token),
):
    """
    Получить временный токен/ссылку для удалённого рабочего стола через MeshCentral.
    """
    try:
        mesh_token = await meshcentral_create_token(agent.uuid)
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Internal error: {str(e)}"
        )

    # Возвращаем токен для клиента (Web UI может сгенерировать WebRTC/RDP ссылку)
    return {"agent_uuid": agent.uuid, "mesh_token": mesh_token}
