from fastapi import FastAPI, WebSocket, WebSocketDisconnect, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.templating import Jinja2Templates
import os
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("rmm")

app = FastAPI()

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
templates = Jinja2Templates(directory=os.path.join(BASE_DIR, "templates"))

# We store both agents and viewers by ID
agents: dict[str, WebSocket] = {}


@app.get("/")
async def index(request: Request):
    """Serve the viewer HTML."""
    return templates.TemplateResponse("index.html", {"request": request})


@app.websocket("/ws/agent/{agent_id}")
async def agent_ws(ws: WebSocket, agent_id: str):
    """Handle agent WebSocket connection."""
    await ws.accept()
    agents[agent_id] = ws
    logger.info(f"Agent connected: {agent_id}")

    try:
        while True:
            data = await ws.receive_text()
            viewer = agents.get(f"viewer:{agent_id}")
            if viewer:
                await viewer.send_text(data)
    except WebSocketDisconnect:
        logger.info(f"Agent disconnected: {agent_id}")
    finally:
        agents.pop(agent_id, None)


@app.websocket("/ws/viewer/{agent_id}")
async def viewer_ws(ws: WebSocket, agent_id: str):
    """Handle viewer WebSocket connection."""
    await ws.accept()
    old_viewer = agents.get(f"viewer:{agent_id}")
    if old_viewer:
        await safe_close(old_viewer)

    agents[f"viewer:{agent_id}"] = ws
    logger.info(f"Viewer connected: {agent_id}")

    try:
        while True:
            data = await ws.receive_text()
            agent = agents.get(agent_id)
            if agent:
                await agent.send_text(data)
    except WebSocketDisconnect:
        logger.info(f"Viewer disconnected: {agent_id}")
    finally:
        agents.pop(f"viewer:{agent_id}", None)


async def safe_close(ws: WebSocket):
    """Safely close any existing WebSocket."""
    try:
        await ws.close()
    except Exception as e:
        logger.warning(f"Error closing old viewer: {e}")
