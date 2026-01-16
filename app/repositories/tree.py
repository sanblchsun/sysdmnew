# db/repositories/tree.py
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select
from app.models import Company, Department, Agent


# db/repositories/tree.py
async def get_tree(session: AsyncSession):
    result = await session.execute(select(Company))
    companies = result.scalars().unique().all()

    tree = []
    for c in companies:
        company_node = {
            "id": c.id,  # Используем чисто числовой идентификатор
            "name_id": f"company-{c.id}",
            "name": c.name,
            "type": "company",  # Устанавливаем правильный тип
            "children": [],
        }
        for d in c.departments:
            dept_node = {
                "id": d.id,  # Аналогично используем числовой идентификатор
                "name_id": f"dept-{d.id}",
                "name": d.name,
                "type": "department",  # Указываем тип отделения
                "children": [],
            }
            for a in d.agents:
                agent_node = {
                    "id": a.id,  # Чистое числовое значение
                    "name_id": f"agent-{a.id}",
                    "name": a.name_pc,
                    "type": "agent",  # Тип сотрудника
                    "children": [],
                }
                dept_node["children"].append(agent_node)
            company_node["children"].append(dept_node)
        tree.append(company_node)
    return tree
