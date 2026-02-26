# test/main.py
import base64
import json
import asyncio
import websockets
from fastapi import FastAPI

app = FastAPI()

# MeshCentral settings
MESH_SITE = "wss://localhost:443"
MESH_USERNAME = "~t:M4cUAArHuU6hBGaz"
MESH_PASSWORD = "4dACXjrRgIE0B8SM6jDN"


async def send_mesh_command(command: dict):
    # Encode username and password as base64
    auth_header = f"{base64.b64encode(MESH_USERNAME.encode()).decode()},{base64.b64encode(MESH_PASSWORD.encode()).decode()}"
    headers = [("x-meshauth", auth_header)]

    # Disable SSL verification for self-signed cert
    import ssl

    ssl_context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    ssl_context.check_hostname = False
    ssl_context.verify_mode = ssl.CERT_NONE

    uri = f"{MESH_SITE}/control.ashx"
    async with websockets.connect(uri, extra_headers=headers, ssl=ssl_context) as ws:
        await ws.send(json.dumps(command))
        async for message in ws:
            response = json.loads(message)
            # Return the first response (or filter by responseid)
            return response


@app.get("/mesh/test")
async def mesh_test():
    command = {"action": "info", "responseid": "fastapi_test"}
    response = await send_mesh_command(command)
    return {"mesh_response": response}
