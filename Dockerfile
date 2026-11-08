FROM python:3.12-slim
WORKDIR /service
RUN useradd --create-home miner && mkdir /data && chown miner:miner /data
COPY app ./app
USER miner
ENV HOST=0.0.0.0 PORT=8000 DB_PATH=/data/miner.sqlite3 PYTHONUNBUFFERED=1
EXPOSE 8000
CMD ["python", "-m", "app"]
