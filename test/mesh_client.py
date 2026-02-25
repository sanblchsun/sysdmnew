# test/mesh_client.py
import json
import time
import asyncio
from base64 import b64encode, b64decode
from typing import Optional, Dict, Any, List
import ssl
import hashlib
import binascii

import websockets
from websockets import WebSocketClientProtocol
from Crypto.Cipher import AES
from Crypto.Random import get_random_bytes
from loguru import logger

from test.config import settings


class MeshCentralClient:
    """Клиент для работы с MeshCentral API через WebSocket"""

    def __init__(
        self,
        username: Optional[str] = None,
        password: Optional[str] = None,
        site: Optional[str] = None,
        verify_ssl: bool = False,
    ):

        self.loginuser: str = (
            username if username is not None else settings.MESH_USERNAME
        )
        self.loginpass: str = (
            password if password is not None else settings.MESH_TOKEN_KEY
        )
        self.site: str = site if site is not None else settings.MESH_SITE
        self.verify_ssl = verify_ssl
        self._websocket: Optional[WebSocketClientProtocol] = None
        self._connection_lock = asyncio.Lock()
        self._format_index = 0  # Начинаем с первого формата

        # Извлекаем чистое имя только для логов
        self.username = self._extract_username(self.loginuser)

        # Генерируем ключ AES из пароля (32 байта)
        self.aes_key = self._generate_key_from_password(self.loginpass)

        logger.info(f"Инициализация клиента:")
        logger.info(f"  - Login User (полный): {self.loginuser}")
        logger.info(f"  - Username (чистый): {self.username}")
        logger.info(f"  - Site: {self.site}")
        logger.info(f"  - Password length: {len(self.loginpass)}")
        logger.info(
            f"  - AES key (hex): {binascii.hexlify(self.aes_key[:8]).decode()}..."
        )

    def _get_ssl_context(self) -> Optional[ssl.SSLContext]:
        """Создает SSL контекст с отключенной проверкой"""
        if not self.verify_ssl:
            ssl_context = ssl.create_default_context()
            ssl_context.check_hostname = False
            ssl_context.verify_mode = ssl.CERT_NONE
            return ssl_context
        return None

    def _extract_username(self, raw_username: str) -> str:
        """Извлекает имя пользователя из формата ~t:... (только для логов)"""
        if raw_username.startswith("~t:"):
            return raw_username[3:]
        return raw_username

    def _generate_key_from_password(self, password: str) -> bytes:
        """Генерирует ключ AES из пароля (32 байта)"""
        logger.info(f"Генерация ключа из пароля длиной {len(password)}")

        # Используем SHA256 для получения 32 байт
        key = hashlib.sha256(password.encode("utf-8")).digest()
        logger.info(f"  Использую SHA256 ключ длиной {len(key)} байт")
        return key

    def _get_userid_formats(self) -> List[str]:
        """Возвращает все возможные форматы userid для тестирования"""
        return [
            self.loginuser,  # 0: ~t:gMnbyvOEAAu9oCjp
            f"user//{self.username}",  # 1: user//gMnbyvOEAAu9oCjp
            f"user/{self.username}",  # 2: user/gMnbyvOEAAu9oCjp
            f"user/{self.loginuser}",  # 3: user/~t:gMnbyvOEAAu9oCjp
            f"user//{self.loginuser}",  # 4: user//~t:gMnbyvOEAAu9oCjp
            self.username,  # 5: gMnbyvOEAAu9oCjp
        ]

    def _create_auth_token(self) -> str:
        """
        Создает токен аутентификации - пробуем разные форматы
        """
        try:
            # Получаем все форматы
            formats = self._get_userid_formats()

            # Берем текущий формат для тестирования
            userid = formats[self._format_index]

            # Создаем объект как в meshctrl
            obj = {"userid": userid, "domainid": "", "time": int(time.time())}

            # Преобразуем в JSON без пробелов
            json_str = json.dumps(obj, separators=(",", ":"))
            json_bytes = json_str.encode("utf-8")

            logger.info(f"Создание токена:")
            logger.info(f"  - Format index: {self._format_index}")
            logger.info(f"  - Format: {formats[self._format_index]}")
            logger.info(f"  - Object: {obj}")
            logger.info(f"  - JSON: {json_str}")
            logger.info(f"  - JSON bytes: {len(json_bytes)}")

            # Генерируем IV (12 байт)
            iv = get_random_bytes(12)

            # Берем первые 32 байта ключа
            key = self.aes_key[:32]

            # Шифруем
            cipher = AES.new(key, AES.MODE_GCM, iv)
            encrypted, tag = cipher.encrypt_and_digest(json_bytes)

            logger.info(f"  - IV: {binascii.hexlify(iv).decode()}")
            logger.info(f"  - Encrypted length: {len(encrypted)}")
            logger.info(f"  - Tag: {binascii.hexlify(tag).decode()}")

            # Формируем результат: iv + tag + encrypted
            result = iv + tag + encrypted
            logger.info(f"  - Result length: {len(result)}")

            # Base64 с заменой символов
            token = b64encode(result).decode("ascii")
            token = token.replace("+", "@").replace("/", "$")

            logger.info(f"  - Token length: {len(token)}")
            logger.info(f"  - Token (first 50): {token[:50]}...")

            return token

        except Exception as e:
            logger.error(f"Ошибка создания токена: {e}")
            raise

    async def _get_connection(self) -> WebSocketClientProtocol:
        """Получает или создает WebSocket соединение"""
        async with self._connection_lock:
            if self._websocket is None or self._websocket.closed:
                token = self._create_auth_token()
                uri = f"{self.site}/control.ashx?auth={token}"

                logger.info(f"Создание нового соединения...")

                ssl_context = self._get_ssl_context()
                try:
                    self._websocket = await websockets.connect(
                        uri,
                        ssl=ssl_context,
                        ping_interval=20,
                        ping_timeout=20,
                        close_timeout=10,
                    )
                    logger.success("Новое соединение создано")
                except Exception as e:
                    logger.error(f"Ошибка подключения: {e}")
                    raise

            return self._websocket

    async def send_command(self, command: Dict[str, Any]) -> Dict[str, Any]:
        """Отправка команды с автоматическим перебором форматов"""
        max_formats = len(self._get_userid_formats())

        for attempt in range(max_formats):
            try:
                # Устанавливаем текущий формат
                self._format_index = attempt

                # Сбрасываем соединение для нового формата
                await self.disconnect()

                # Создаем новое соединение с новым форматом
                websocket = await self._get_connection()

                if "responseid" not in command:
                    command["responseid"] = f"cmd_{int(time.time())}"

                logger.info(f"→ Попытка {attempt + 1}/{max_formats}: {command}")

                await websocket.send(json.dumps(command))
                response = await websocket.recv()
                result = json.loads(response)

                logger.info(f"← Получен ответ: {result}")

                # Если получили не noauth или это последняя попытка, возвращаем результат
                if result.get("cause") != "noauth" or attempt == max_formats - 1:
                    return result

                # Иначе пробуем следующий формат
                logger.warning(f"Формат {attempt} вернул noauth, пробуем следующий...")

            except Exception as e:
                logger.error(f"Ошибка при попытке {attempt + 1}: {e}")
                if attempt == max_formats - 1:
                    raise
                continue

        return {"error": "Все форматы не сработали"}

    async def get_server_info(self) -> Dict[str, Any]:
        """Получение информации о сервере"""
        return await self.send_command({"action": "info"})

    async def disconnect(self):
        """Закрытие соединения"""
        async with self._connection_lock:
            if self._websocket and not self._websocket.closed:
                await self._websocket.close()
                self._websocket = None
                logger.info("Соединение закрыто")

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        await self.disconnect()


# Создаем клиент
mesh_client = MeshCentralClient(verify_ssl=False)
