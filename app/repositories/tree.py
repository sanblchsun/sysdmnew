# db/repositories/tree.py
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select
from app.models import Company, Department, Agent


async def get_tree(session: AsyncSession):
    result = await session.execute(select(Company))
    companies = result.scalars().unique().all()

    tree = []
    for c in companies:
        company_node = {
            "id": f"company-{c.id}",
            "name": c.name,
            "type": "office",
            "children": [],
        }
        for d in c.departments:
            dept_node = {
                "id": f"dept-{d.id}",
                "name": d.name,
                "type": "office",
                "children": [],
            }
            for a in d.agents:
                agent_node = {
                    "id": f"agent-{a.id}",
                    "name": a.name,
                    "type": "agent",
                    "children": [],
                }
                dept_node["children"].append(agent_node)
            company_node["children"].append(dept_node)
        tree.append(company_node)
    return tree
