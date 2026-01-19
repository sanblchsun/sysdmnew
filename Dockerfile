# Dockerfile
# ============================================
# SysDM - System Management Docker Image
# ============================================

FROM python:3.12.3-slim

WORKDIR /app

RUN apt-get update && apt-get install -y

COPY ./requirements.txt /app/requirements.txt

RUN pip install --no-cache-dir -r requirements.txt

COPY ./app /app

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
