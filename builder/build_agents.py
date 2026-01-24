import asyncio
import os
import subprocess
from pathlib import Path

from sqlalchemy import select

from app.config import settings
from app.database import AsyncSessionLocal
from app.models import Company

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
    """
    Преобразует имя компании в "slug"
    """
    return name.lower().replace(" ", "_").replace("-", "_")


# ===================== BUILD =====================
def build_exe(company_slug: str, config: dict) -> None:
    """
    Собирает Windows exe для конкретной компании
    с вшивкой CompanyIDStr, ServerURL и BuildSlug.
    """
    output_exe = DIST_DIR / f"agent_{company_slug}.exe"

    # ldflags для передачи compile-time переменных в Go
    ldflags = (
        f"-X main.CompanyIDStr={config['company_id']} "
        f"-X main.ServerURL={config['server_url']} "
        f"-X main.BuildSlug={company_slug}"
    )

    print(f"[+] Building {output_exe.name}")

    subprocess.run(
        [
            "go",
            "build",
            "-o",
            str(output_exe),
            "-ldflags",
            ldflags,
            str(GO_ENTRYPOINT),
        ],
        cwd=GO_AGENT_DIR,
        env={
            **os.environ,
            "GOOS": GOOS,
            "GOARCH": GOARCH,
        },
        check=True,
    )


# ===================== MAIN =====================
async def build_all_agents() -> None:
    """
    Берёт все компании из базы и собирает exe для каждой.
    """
    async with AsyncSessionLocal() as session:
        result = await session.execute(select(Company))
        companies = result.scalars().all()

        if not companies:
            print("[!] No companies found")
            return

        for company in companies:
            slug = getattr(company, "slug", None) or slugify(company.name)
            print(f"[*] Building agent for {slug} (id={company.id})")

            config = {
                "company_id": company.id,
                "server_url": settings.APP_HOST,
            }

            build_exe(slug, config)


if __name__ == "__main__":
    asyncio.run(build_all_agents())
