import asyncio
from sqlalchemy import select

from app.database import AsyncSessionLocal as new_session
from app.models import User
from app.config import settings


async def create_user(
    username: str,
    password: str,
    is_active: bool = True,
):
    async with new_session() as session:
        result = await session.execute(select(User).where(User.username == username))
        existing_user = result.scalar_one_or_none()

        if existing_user:
            print(f"❌ Пользователь '{username}' уже существует")
            return

        user = User(
            username=username,
            is_active=is_active,
        )
        user.set_password(password)

        session.add(user)
        await session.commit()

        print(f"✅ Пользователь '{username}' успешно создан")


if __name__ == "__main__":
    asyncio.run(
        create_user(
            username=settings.FIRST_SUPERUSER,
            password=settings.FIRST_SUPERUSER_PASSWORD,
            is_active=True,
        )
    )
