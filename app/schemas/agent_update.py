# app/schemas/agent_update.py
from pydantic import BaseModel
from typing import Optional


class AgentCheckUpdateIn(BaseModel):
    build: str


class AgentCheckUpdateOut(BaseModel):
    update: bool
    build: Optional[str] = None
    url: Optional[str] = None
    sha256: Optional[str] = None
    force: bool = False
