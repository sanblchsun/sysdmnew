# app/core/meshcentral.py
import httpx
import os

MESH_BASE_URL = os.getenv("MESH_URL", "https://meshcentral.example.com")
MESH_API_KEY = os.getenv("MESH_API_KEY", "supersecret")


async def meshcentral_create_token(agent_uuid: str) -> str:
    """
    Создаёт временный токен для удалённого рабочего стола агента через MeshCentral API.
    """
    async with httpx.AsyncClient(verify=False, timeout=5.0) as client:
        resp = await client.post(
            f"{MESH_BASE_URL}/api/tokens",
            headers={"Authorization": f"Bearer {MESH_API_KEY}"},
            json={
                "account": agent_uuid,
                "permissions": ["remotecontrol"],
                "expire": 300,  # 5 минут
            },
        )
        resp.raise_for_status()
        data = resp.json()
        return data["token"]
