# app/models.py
from pydantic import BaseModel
from sqlalchemy import JSON, ForeignKey, String
from sqlalchemy.orm import Mapped, mapped_column, relationship
from app.database import Base
from passlib.context import CryptContext


class Company(Base):
    __tablename__ = "companies"

    id: Mapped[int] = mapped_column(primary_key=True)
    name: Mapped[str] = mapped_column(String(255), unique=True)

    departments: Mapped[list["Department"]] = relationship(
        back_populates="company", lazy="selectin"
    )


class Department(Base):
    __tablename__ = "departments"

    id: Mapped[int] = mapped_column(primary_key=True)
    name: Mapped[str] = mapped_column(String(255))

    company_id: Mapped[int] = mapped_column(
        ForeignKey("companies.id", ondelete="CASCADE")
    )

    company: Mapped["Company"] = relationship(back_populates="departments")
    agents: Mapped[list["Agent"]] = relationship(
        back_populates="department", lazy="selectin"
    )


class Agent(Base):
    __tablename__ = "agents"

    id: Mapped[int] = mapped_column(primary_key=True)
    system: Mapped[str] = mapped_column(String(255), nullable=True)
    name_pc: Mapped[str] = mapped_column(String(255))
    user_name: Mapped[str] = mapped_column(String(255), nullable=True)
    ip_addr: Mapped[str] = mapped_column(String(15), nullable=True)

    department_id: Mapped[int] = mapped_column(
        ForeignKey("departments.id", ondelete="CASCADE")
    )

    department: Mapped["Department"] = relationship(back_populates="agents")

    # Связь с дополнительными данными
    additional_data: Mapped["AgentAdditionalData"] = relationship(
        back_populates="agent", uselist=False, cascade="all, delete-orphan"
    )


class DiskInfo(BaseModel):
    name: str
    size: int
    free: int


class AgentAdditionalData(Base):
    __tablename__ = "agent_additional_data"

    id: Mapped[int] = mapped_column(primary_key=True)
    agent_id: Mapped[int] = mapped_column(ForeignKey("agents.id"))
    agent: Mapped["Agent"] = relationship(back_populates="additional_data")

    # Массив дисков (каждый диск представлен отдельным объектом)
    disks: Mapped[list[DiskInfo]] = mapped_column(JSON)

    # Информация о памяти
    total_memory: Mapped[int] = mapped_column(nullable=True)
    available_memory: Mapped[int] = mapped_column(nullable=True)

    # Внешний IP
    external_ip: Mapped[str] = mapped_column(String(15), nullable=True)


pwd_context = CryptContext(schemes=["bcrypt"], deprecated="auto")


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
