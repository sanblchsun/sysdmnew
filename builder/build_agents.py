# builder/build_agents.py

import asyncio
import os
import subprocess
import hashlib
from pathlib import Path

from sqlalchemy import select, desc, update
from sqlalchemy.ext.asyncio import AsyncSession

from app.config import settings
from app.database import AsyncSessionLocal
from app.models import Company, AgentBuild


# ===================== PATHS =====================

PROJECT_ROOT = Path(__file__).resolve().parent
GO_AGENT_DIR = PROJECT_ROOT / "agent"
DIST_DIR = PROJECT_ROOT / "dist" / "agents"
DIST_DIR.mkdir(parents=True, exist_ok=True)

GO_ENTRYPOINT = GO_AGENT_DIR / "cmd" / "agent"
GOOS = "windows"
GOARCH = "amd64"


# ===================== HELPERS =====================


def slugify(name: str) -> str:
    return name.lower().replace(" ", "_").replace("-", "_")


def increment_build_slug(last_slug: str | None) -> str:
    """
    Формат: 1.0.X
    """
    if not last_slug:
        return "1.0.0"

    parts = last_slug.split(".")
    if len(parts) != 3:
        return "1.0.0"

    major, minor, patch = map(int, parts)
    patch += 1
    return f"{major}.{minor}.{patch}"


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(8192), b""):
            h.update(chunk)
    return h.hexdigest()


def build_exe(company_slug: str, build_slug: str, config: dict) -> Path:
    output_exe = DIST_DIR / f"agent_{company_slug}_{build_slug}.exe"

    ldflags = (
        f"-X main.CompanyIDStr={config['company_id']} "
        f"-X main.CompanySlug={company_slug} "
        f"-X main.ServerURL={config['server_url']} "
        f"-X main.BuildSlug={build_slug}"
    )

    print(f"[+] Building {output_exe.name}")

    subprocess.run(
        ["go", "build", "-o", str(output_exe), "-ldflags", ldflags, str(GO_ENTRYPOINT)],
        cwd=GO_AGENT_DIR,
        env={**os.environ, "GOOS": GOOS, "GOARCH": GOARCH},
        check=True,
    )

    return output_exe


async def activate_build(session: AsyncSession, build_id: int):
    """
    Гарантированно оставляет только одну запись с is_active=True
    """

    # снимаем active со всех
    await session.execute(
        update(AgentBuild).where(AgentBuild.is_active.is_(True)).values(is_active=False)
    )

    # активируем новую
    await session.execute(
        update(AgentBuild).where(AgentBuild.id == build_id).values(is_active=True)
    )

    await session.commit()


# ===================== MAIN =====================


async def build_all_agents() -> None:
    async with AsyncSessionLocal() as session:

        # 1️⃣ Получаем последний билд
        result = await session.execute(select(AgentBuild).order_by(desc(AgentBuild.id)))
        last_build = result.scalars().first()
        last_slug = last_build.build_slug if last_build else None

        new_build_slug = increment_build_slug(last_slug)
        print(f"[i] New build slug: {new_build_slug}")

        # 2️⃣ Получаем компании
        result = await session.execute(select(Company))
        companies = result.scalars().all()

        if not companies:
            print("[!] No companies found")
            return

        # 3️⃣ Строим exe для всех компаний
        first_exe_path: Path | None = None

        for company in companies:
            slug = getattr(company, "slug", None) or slugify(company.name)

            config = {
                "company_id": company.id,
                "server_url": settings.APP_HOST,
            }

            exe_path = build_exe(slug, new_build_slug, config)

            if first_exe_path is None:
                first_exe_path = exe_path

        # 4️⃣ Считаем sha256 (один раз)
        if not first_exe_path:
            print("[!] No exe built")
            return

        sha256 = sha256_file(first_exe_path)
        print(f"[i] SHA256: {sha256}")

        # 5️⃣ Создаём запись билда (пока не active)
        build = AgentBuild(
            build_slug=new_build_slug,
            sha256=sha256,
            is_active=False,
        )

        session.add(build)
        await session.flush()  # получаем build.id без commit

        # 6️⃣ Активируем билд (снимаем старый active)
        await activate_build(session, build.id)

        print(f"[✔] All agents built for build {new_build_slug}")


if __name__ == "__main__":
    asyncio.run(build_all_agents())
