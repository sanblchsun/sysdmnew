# app/schemas/agent.py
from pydantic import BaseModel
from typing import Optional, List


class DiskInfoSchema(BaseModel):
    name: str
    size: int
    free: int


class AgentRegisterIn(BaseModel):
    name_pc: str
    department_id: int

    system: Optional[str] = None
    user_name: Optional[str] = None
    ip_addr: Optional[str] = None

    disks: List[DiskInfoSchema] = []
    total_memory: Optional[int] = None
    available_memory: Optional[int] = None
    external_ip: Optional[str] = None


class AgentRegisterOut(BaseModel):
    agent_uuid: str
    token: str
