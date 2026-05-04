# Объединенный агент sysdmnew + rmm_cpp

Этот агент интегрирует функциональность обоих проектов:

## Функции из sysdmnew

- ✅ Регистрация на сервере (POST `/api/agent/register`)
- ✅ Heartbeat (POST `/api/agent/heartbeat`)
- ✅ Телеметрия (POST `/api/agent/telemetry`)
- ✅ Проверка обновлений (GET `/api/agent/check-update`)
- ✅ Автоматическое обновление exe
- ✅ Windows сервис (AutoStart)
- ✅ Логирование в файл

## Функции из rmm_cpp (ТБА)

- ⏳ Видеопоток MJPEG/H.264
- ⏳ WebSocket для управления (мышь, клавиатура, буфер обмена)
- ⏳ Конфигурация кодека через сервер
- ⏳ Screen resolution detection

## Архитектура

```
agent_universal.exe (скомпилировано с параметрами)
│
├── Регистрация + Heartbeat + Телеметрия (sysdmnew)
│   └── Сохраняет UUID и Token для следующих обновлений
│
├── Автоматическое обновление (sysdmnew)
│   └── Проверка каждые 60 сек, скачивание, верификация SHA256
│
├── Windows Сервис
│   └── AutoStart, restart on failure
│
└── Видеопоток (ТБА - rmm_cpp)
    └── /ingest/{agent_id}
    └── /ws/control/agent/{agent_id}
```

## Компиляция

### Linux (для Windows exe)

```bash
# Установить MinGW
sudo apt install mingw-w64

# Скомпилировать
cd builder_cpp/agent/cmd/agent/
g++ -O2 -std=c++17 -static -static-libgcc -static-libstdc++ \
    -DSERVER_URL=\"https://dev.local\" \
    -DBUILD_SLUG=\"1.0.0\" \
    -DAGENT_ID=\"agent_universal\" \
    main_unified.cpp -o agent.exe \
    -lws2_32 -luser32 -lsecur32 -lcrypt32 -ladvapi32 -lwinhttp
```

### Windows (MSVC)

```bash
# В "x64 Native Tools Command Prompt for VS"
cd sysdmnew\rmm_cpp\agent
build.bat
```

### Windows (MinGW)

```bash
# Убедитесь что g++ установлен (msys2/mingw-w64)
cd sysdmnew\rmm_cpp\agent
build.bat
```

## Запуск

### Первый запуск (установка сервиса)

```bash
# От имени администратора
cd C:\Users\user\Desktop\build\rmm_cpp\agent
agent.exe
```

Первый запуск:

1. Проверяет, установлен ли сервис
2. Если нет - устанавливает как сервис "SystemMonitoringAgent"
3. Запускает сервис (AutoStart на следующую загрузку)
4. Регистрируется на сервере
5. Отправляет телеметрию
6. Входит в основной цикл (heartbeat, update checks)

### Последующие запуски

Сервис запускается автоматически при загрузке Windows.

Логи находятся в:

```
C:\Users\user\Desktop\build\rmm_cpp\agent\agent.log
```

## Параметры компиляции

Параметры вшиваются при компиляции через макросы препроцессора:

- `SERVER_URL` - адрес сервера (по умолчанию `https://dev.local`)
- `BUILD_SLUG` - версия агента (по умолчанию `1.0.0`)
- `AGENT_ID` - уникальный ID агента (по умолчанию `agent_universal`)

Примеры:

```bash
-DSERVER_URL=\"https://production.example.com\"
-DBUILD_SLUG=\"2.1.0\"
-DAGENT_ID=\"agent-office-1\"
```

## Эндпоинты сервера

### Регистрация

```
POST /api/agent/register
{
  "name_pc": "DESKTOP-ABC123",
  "machine_uid": "1234567890-12345-678",
  "exe_version": "1.0.0",
  "external_ip": "203.0.113.42"
}

Response:
{
  "agent_uuid": "550e8400-e29b-41d4-a716-446655440000",
  "token": "abc123def456"
}
```

### Heartbeat

```
POST /api/agent/heartbeat?uuid={uuid}&token={token}
{}

Response:
{
  "telemetry_mode": "full" | "basic" | "none"
}
```

### Телеметрия

```
POST /api/agent/telemetry?uuid={uuid}&token={token}
{
  "system": "windows",
  "user_name": "User1, User2",
  "ip_addr": "192.168.1.10",
  "external_ip": "203.0.113.42",
  "total_memory": 16384,
  "available_memory": 8192,
  "exe_version": "1.0.0"
}
```

### Проверка обновлений

```
GET /api/agent/check-update?uuid={uuid}&token={token}
{}

Response (если обновление есть):
{
  "update": true,
  "build": "1.1.0",
  "url": "https://..../agent_1.1.0.exe",
  "sha256": "abc123..."
}
```

### Конфигурация видеопотока (ТБА)

```
GET /agents/{agent_id}/config

Response:
codec=mjpeg
encoder=cpu
bitrate=4M
fps=30
mjpeg_q=4
```

## Структура файлов

```
sysdmnew/
├── builder_cpp/
│   └── agent/
│       ├── cmd/agent/
│       │   ├── main.cpp (старый, для совместимости)
│       │   └── main_unified.cpp (новый объединенный)
│       └── build_agents.py (обновлен)
│
└── rmm_cpp/
    └── agent/
        ├── main.cpp (старый rmm_cpp агент, больше не используется)
        ├── build.bat (обновлен на main_unified.cpp)
        ├── run.bat, runssl.bat, runmyssl.bat (обновлены)
        └── test_connection.bat (диагностика)
```

## TODO: Функции rmm_cpp

Следующий этап - добавить в main_unified.cpp:

1. **TLS/Schannel** - HTTPS и WSS поддержка
2. **HTTP GET** - получение конфигурации видеопотока
3. **WebSocket** - управление (мышь, клавиатура, буфер обмена)
4. **ffmpeg интеграция** - захват видео и кодирование
5. **Screen metrics** - отслеживание разрешения экрана
6. **Control handlers** - обработка команд мыши/клавиатуры
