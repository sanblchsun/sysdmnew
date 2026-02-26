from fastapi import FastAPI, WebSocket
from fastapi.middleware.cors import CORSMiddleware

app = FastAPI()

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

agents = {}


@app.websocket("/ws/agent/{agent_id}")
async def agent_ws(ws: WebSocket, agent_id: str):
    await ws.accept()
    agents[agent_id] = ws
    try:
        while True:
            data = await ws.receive_text()
            viewer = agents.get(f"viewer:{agent_id}")
            if viewer:
                await viewer.send_text(data)
    except:
        del agents[agent_id]


@app.websocket("/ws/view/{agent_id}")
async def viewer_ws(ws: WebSocket, agent_id: str):
    await ws.accept()
    agents[f"viewer:{agent_id}"] = ws
    try:
        while True:
            data = await ws.receive_text()
            agent = agents.get(agent_id)
            if agent:
                await agent.send_text(data)
    except:
        del agents[f"viewer:{agent_id}"]
