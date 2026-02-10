# app/schemas/agent.py
from pydantic import BaseModel
from typing import Optional, List


class DiskInfoSchema(BaseModel):
    name: str
    size: int
    free: int


class AgentRegisterIn(BaseModel):
    machine_uid: str
    name_pc: str
    company_id: int

    exe_version: str | None = None

    system: str | None = None
    user_name: str | None = None
    ip_addr: str | None = None

    disks: list[DiskInfoSchema] = []
    total_memory: int | None = None
    available_memory: int | None = None
    external_ip: str | None = None


class AgentRegisterOut(BaseModel):
    agent_uuid: str
    token: str


class AgentTelemetryIn(BaseModel):
    system: str | None = None
    user_name: str | None = None
    ip_addr: str | None = None
    disks: list[DiskInfoSchema] = []
    total_memory: int | None = None
    available_memory: int | None = None
    external_ip: str | None = None
    exe_version: str | None = None
