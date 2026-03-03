# rmm/main.py
from fastapi import FastAPI, WebSocket, WebSocketDisconnect
from fastapi.middleware.cors import CORSMiddleware
from fastapi.templating import Jinja2Templates
from fastapi.requests import Request
import os

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

agents = {}


@app.get("/")
async def index(request: Request):
    return templates.TemplateResponse("index.html", {"request": request})


@app.websocket("/ws/agent/{agent_id}")
async def agent_ws(ws: WebSocket, agent_id: str):
    await ws.accept()
    agents[agent_id] = ws
    print("Agent connected:", agent_id)

    try:
        while True:
            data = await ws.receive_text()
            # Forward data to viewer
            viewer = agents.get(f"viewer:{agent_id}")
            if viewer:
                await viewer.send_text(data)
    except WebSocketDisconnect:
        print("Agent disconnected:", agent_id)
    finally:
        agents.pop(agent_id, None)


@app.websocket("/ws/viewer/{agent_id}")
async def viewer_ws(ws: WebSocket, agent_id: str):
    await ws.accept()

    # Close old viewer if exists
    old_viewer = agents.get(f"viewer:{agent_id}")
    if old_viewer:
        try:
            await old_viewer.close()
        except:
            pass

    agents[f"viewer:{agent_id}"] = ws
    print("Viewer connected:", agent_id)

    try:
        while True:
            data = await ws.receive_text()
            agent = agents.get(agent_id)
            if agent:
                await agent.send_text(data)
    except WebSocketDisconnect:
        print("Viewer disconnected:", agent_id)
    finally:
        agents.pop(f"viewer:{agent_id}", None)