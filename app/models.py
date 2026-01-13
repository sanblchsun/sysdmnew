from sqlalchemy import ForeignKey, String
from sqlalchemy.orm import Mapped, mapped_column, relationship
from app.database import Base

class Company(Base):
    __tablename__ = "companies"

    id: Mapped[int] = mapped_column(primary_key=True)
    name: Mapped[str] = mapped_column(String(255), unique=True)

    departments: Mapped[list["Department"]] = relationship(
        back_populates="company",
        lazy="selectin"
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
        back_populates="department",
        lazy="selectin"
    )


class Agent(Base):
    __tablename__ = "agents"

    id: Mapped[int] = mapped_column(primary_key=True)
    name: Mapped[str] = mapped_column(String(255))

    department_id: Mapped[int] = mapped_column(
        ForeignKey("departments.id", ondelete="CASCADE")
    )

    department: Mapped["Department"] = relationship(back_populates="agents")