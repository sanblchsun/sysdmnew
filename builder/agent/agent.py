import asyncio
import json
import cv2
import numpy as np
import mss
import av
from fractions import Fraction
from aiortc import (
    RTCPeerConnection,
    RTCSessionDescription,
    MediaStreamTrack,
)
from aiortc.sdp import candidate_from_sdp
import websockets

SIGNALING_URL = "ws://192.168.2.191:8000/ws/agent/my-agent-id"  # адрес твоего сервера


class ScreenTrack(MediaStreamTrack):
    """Захватывает экран и отдает как видео-поток."""

    kind = "video"

    def __init__(self, fps=10):
        super().__init__()
        self.sct = mss.mss()
        self.monitor = self.sct.monitors[1]
        self.fps = fps
        self._timestamp = 0

    async def recv(self):
        frame = self.sct.grab(self.monitor)
        img = np.array(frame, dtype=np.uint8)
        img = cv2.cvtColor(img, cv2.COLOR_BGRA2BGR)
        img = img.astype(np.uint8, copy=False)

        video_frame = av.VideoFrame.from_ndarray(img, format="bgr24")
        self._timestamp += 1
        video_frame.pts = self._timestamp
        video_frame.time_base = Fraction(
            1, self.fps
        )  # ✅ используем Fraction, не float

        await asyncio.sleep(1 / self.fps)
        return video_frame


async def run_agent():
    async with websockets.connect(SIGNALING_URL) as ws:
        pc = RTCPeerConnection()
        pc.addTrack(ScreenTrack())

        @pc.on("icecandidate")
        async def on_icecandidate(event):
            if event.candidate:
                # Передаём кандидата как обычную строку SDP
                await ws.send(
                    json.dumps(
                        {
                            "type": "candidate",
                            "candidate": event.candidate.to_sdp(),
                            "sdpMid": event.candidate.sdp_mid,
                            "sdpMLineIndex": event.candidate.sdp_mline_index,
                        }
                    )
                )

        async for message in ws:
            data = json.loads(message)

            if data["type"] == "offer":
                offer = RTCSessionDescription(sdp=data["sdp"], type="offer")
                await pc.setRemoteDescription(offer)

                answer = await pc.createAnswer()
                await pc.setLocalDescription(answer)
                await ws.send(
                    json.dumps(
                        {
                            "type": "answer",
                            "sdp": pc.localDescription.sdp,
                        }
                    )
                )

            elif data["type"] == "candidate":
                cand_sdp = data.get("candidate")
                if cand_sdp:
                    try:
                        # корректный способ преобразовать SDP в RTCIceCandidate
                        ice = candidate_from_sdp(cand_sdp)
                        await pc.addIceCandidate(ice)
                    except Exception as e:
                        print(f"⚠️ Ошибка добавления ICE-кандидата: {e}")


if __name__ == "__main__":
    asyncio.run(run_agent())
