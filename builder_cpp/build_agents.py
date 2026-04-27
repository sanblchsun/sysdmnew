import asyncio
import os
import subprocess
import hashlib
from pathlib import Path

from sqlalchemy import select, desc, update
from sqlalchemy.ext.asyncio import AsyncSession

from app.config import settings
from app.database import AsyncSessionLocal
from app.models import AgentBuild

PROJECT_ROOT = Path(__file__).resolve().parent
CPP_AGENT_DIR = PROJECT_ROOT / "agent"
DIST_DIR = PROJECT_ROOT / "dist" / "agents"
DIST_DIR.mkdir(parents=True, exist_ok=True)

CPP_ENTRYPOINT = CPP_AGENT_DIR / "cmd" / "agent" / "main.cpp"
GOOS = "windows"
GOARCH = "amd64"

GXX = "C:/msys64/ucrt64/bin/g++.exe"


def increment_build_slug(last_slug: str | None) -> str:
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


def build_exe(build_slug: str) -> Path:
    output_exe = DIST_DIR / f"agent_universal_{build_slug}.exe"

    print(f"[+] Building {output_exe.name}")

    cmd = [
        GXX,
        "-o", str(output_exe),
        str(CPP_ENTRYPOINT),
        "-lwinhttp",
        "-lws2_32",
        "-ladvapi32",
    ]

    subprocess.run(cmd, cwd=str(CPP_AGENT_DIR / "cmd" / "agent"), check=True)

    return output_exe


async def activate_build(session: AsyncSession, build_id: int):
    await session.execute(
        update(AgentBuild).where(AgentBuild.is_active.is_(True)).values(is_active=False)
    )
    await session.execute(
        update(AgentBuild).where(AgentBuild.id == build_id).values(is_active=True)
    )
    await session.commit()


async def build_agent() -> None:
    async with AsyncSessionLocal() as session:
        result = await session.execute(select(AgentBuild).order_by(desc(AgentBuild.id)))
        last_build = result.scalars().first()
        last_slug = last_build.build_slug if last_build else None

        new_build_slug = increment_build_slug(last_slug)
        print(f"[i] New build slug: {new_build_slug}")

        exe_path = build_exe(new_build_slug)

        sha256 = sha256_file(exe_path)
        print(f"[i] SHA256: {sha256}")

        build = AgentBuild(
            build_slug=new_build_slug,
            sha256=sha256,
            is_active=False,
        )
        session.add(build)
        await session.flush()

        await activate_build(session, build.id)

        print(f"[+] Universal agent built: {exe_path.name}")


if __name__ == "__main__":
    asyncio.run(build_agent())