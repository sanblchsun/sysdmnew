# app/models.py
from datetime import datetime
from sqlalchemy import JSON, ForeignKey, String, DateTime, Boolean, Integer
from sqlalchemy.orm import Mapped, mapped_column, relationship
from app.database import Base
from passlib.context import CryptContext
import uuid
from typing import Any, Dict, List, Optional

pwd_context = CryptContext(schemes=["bcrypt"], deprecated="auto")


class Company(Base):
    __tablename__ = "companies"

    id: Mapped[int] = mapped_column(primary_key=True)
    name: Mapped[str] = mapped_column(String(255), unique=True, nullable=False)
    slug: Mapped[str] = mapped_column(String(100), unique=True, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=datetime.utcnow)

    # Отношения
    departments: Mapped[list["Department"]] = relationship(
        back_populates="company",
        lazy="selectin",
        cascade="all, delete-orphan",
    )

    agents: Mapped[list["Agent"]] = relationship(
        back_populates="company",
        lazy="selectin",
        cascade="all, delete-orphan",
    )


class Department(Base):
    __tablename__ = "departments"

    id: Mapped[int] = mapped_column(primary_key=True)
    name: Mapped[str] = mapped_column(String(255), nullable=False)
    company_id: Mapped[int] = mapped_column(
        ForeignKey("companies.id", ondelete="CASCADE"),
        nullable=False,
    )

    # Отношения
    company: Mapped["Company"] = relationship(back_populates="departments")
    agents: Mapped[list["Agent"]] = relationship(
        back_populates="department",
        lazy="selectin",
        cascade="all, delete-orphan",
    )


class Agent(Base):
    __tablename__ = "agents"

    id: Mapped[int] = mapped_column(primary_key=True)
    uuid: Mapped[str] = mapped_column(
        String(36),
        unique=True,
        index=True,
        default=lambda: str(uuid.uuid4()),
    )
    name_pc: Mapped[str] = mapped_column(String(255), nullable=False)

    # Связь с Company
    company_id: Mapped[int] = mapped_column(
        ForeignKey("companies.id", ondelete="CASCADE"), nullable=False, index=True
    )
    company: Mapped["Company"] = relationship(back_populates="agents")

    # Department может быть пустым (Unassigned)
    department_id: Mapped[Optional[int]] = mapped_column(
        ForeignKey("departments.id", ondelete="SET NULL"), nullable=True
    )
    department: Mapped[Optional["Department"]] = relationship(back_populates="agents")

    # 1:1 системная информация
    additional_data: Mapped[Optional["AgentAdditionalData"]] = relationship(
        back_populates="agent", uselist=False, cascade="all, delete-orphan"
    )

    # Аутентификация агента
    token: Mapped[str] = mapped_column(String(128), nullable=False, default="")

    # Статус
    is_active: Mapped[bool] = mapped_column(Boolean, default=True)
    last_seen: Mapped[datetime] = mapped_column(DateTime, default=datetime.utcnow)

    # Версия билда exe
    exe_version: Mapped[Optional[str]] = mapped_column(String(50), nullable=True)


class AgentAdditionalData(Base):
    __tablename__ = "agent_additional_data"

    id: Mapped[int] = mapped_column(primary_key=True)
    agent_id: Mapped[int] = mapped_column(
        ForeignKey("agents.id", ondelete="CASCADE"), unique=True, nullable=False
    )
    agent: Mapped["Agent"] = relationship(back_populates="additional_data")

    # ===== Системная информация =====
    system: Mapped[Optional[str]] = mapped_column(String(255), nullable=True)
    user_name: Mapped[Optional[str]] = mapped_column(String(255), nullable=True)
    ip_addr: Mapped[Optional[str]] = mapped_column(String(45), nullable=True)  # IPv6
    external_ip: Mapped[Optional[str]] = mapped_column(String(45), nullable=True)

    # ===== Ресурсы =====
    disks: Mapped[Optional[List[Dict[str, Any]]]] = mapped_column(JSON, nullable=True)
    total_memory: Mapped[Optional[int]] = mapped_column(nullable=True)
    available_memory: Mapped[Optional[int]] = mapped_column(nullable=True)


class User(Base):
    __tablename__ = "users"

    id: Mapped[int] = mapped_column(primary_key=True)
    username: Mapped[str] = mapped_column(unique=True, nullable=False)
    password_hash: Mapped[str] = mapped_column(nullable=False)
    is_active: Mapped[bool] = mapped_column(default=True)

    def set_password(self, password: str) -> None:
        self.password_hash = pwd_context.hash(password)

    def verify_password(self, password: str) -> bool:
        return pwd_context.verify(password, self.password_hash)
