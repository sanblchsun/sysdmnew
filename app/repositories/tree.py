# db/repositories/tree.py
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select
from sqlalchemy.orm import selectinload
from app.models import Company, Department, Agent


async def get_tree(session: AsyncSession):
    result = await session.execute(
        select(Company).options(
            selectinload(Company.departments).selectinload(Department.agents),
            selectinload(Company.agents),
        )
    )

    companies = result.scalars().unique().all()
    tree = []

    for c in companies:
        company_node = {
            "id": c.id,
            "name_id": f"company-{c.id}",
            "name": c.name,
            "type": "company",
            "children": [],
        }

        # -------------------------
        # 📦 UNASSIGNED
        # -------------------------
        unassigned_agents = [a for a in c.agents if a.department_id is None]

        if unassigned_agents:
            unassigned_node = {
                "id": c.id,
                "name_id": f"unassigned-{c.id}",
                "name": "Без отдела",
                "type": "unassigned",
                "children": [],
            }

            for a in unassigned_agents:
                unassigned_node["children"].append(
                    {
                        "id": a.id,
                        "name_id": f"agent-{a.id}",
                        "name": a.name_pc,
                        "type": "agent",
                        "children": [],
                    }
                )

            company_node["children"].append(unassigned_node)

        # -------------------------
        # 📁 DEPARTMENTS
        # -------------------------
        for d in c.departments:
            dept_node = {
                "id": d.id,
                "name_id": f"dept-{d.id}",
                "name": d.name,
                "type": "department",
                "children": [],
            }

            for a in d.agents:
                dept_node["children"].append(
                    {
                        "id": a.id,
                        "name_id": f"agent-{a.id}",
                        "name": a.name_pc,
                        "type": "agent",
                        "children": [],
                    }
                )

            company_node["children"].append(dept_node)

        tree.append(company_node)

    return tree
