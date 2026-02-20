# app/routers/mesh.py
from fastapi import APIRouter, HTTPException
import requests

from app.config import settings

router = APIRouter(prefix="/mesh", tags=["MeshCentral"])


@router.get("/devices")
def get_mesh_devices():
    """
    Получить список агентов/устройств из MeshCentral
    """
    url = f"{settings.MESH_URL}/mesh/agents?format=json"
    headers = {"Authorization": f"Bearer {settings.MESH_API_KEY}"}

    try:
        resp = requests.get(url, headers=headers, verify=False, timeout=10)
        resp.raise_for_status()
    except requests.exceptions.RequestException as e:
        raise HTTPException(
            status_code=502, detail=f"Error connecting to MeshCentral: {e}"
        )

    try:
        return resp.json()
    except ValueError:
        raise HTTPException(
            status_code=502, detail="Invalid JSON received from MeshCentral"
        )
