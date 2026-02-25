# test/routers/mesh.py
from fastapi import APIRouter, HTTPException, Query
from typing import Dict, Any, List
from loguru import logger
from test.mesh_client import mesh_client

router = APIRouter(prefix="/mesh", tags=["MeshCentral"])


# test/routers/mesh.py (исправленный)
@router.get("/test")
async def test_connection() -> Dict[str, Any]:
    """Тест подключения к MeshCentral"""
    try:
        # НЕ ИСПОЛЬЗУЕМ async with - он закрывает соединение!
        info = await mesh_client.get_server_info()

        return {
            "status": "connected",
            "response": info,
            "server_name": info.get("name", "unknown"),
            "timestamp": info.get("serverTime", 0),
        }
    except Exception as e:
        return {"status": "error", "error": str(e)}
