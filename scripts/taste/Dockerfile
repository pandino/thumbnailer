# Preference-model worker, CPU only (runs on shun, a Celeron N5105 without AVX).
# Build context: scripts/taste (task taste-build).
FROM docker.io/python:3.13-slim

# CPU wheels first: the default PyPI torch pulls several GB of CUDA libraries.
# Versions as tested on shun (2026-10-21).
RUN pip install --no-cache-dir --index-url https://download.pytorch.org/whl/cpu \
        torch==2.14.1 torchvision==0.29.1 \
    && pip install --no-cache-dir \
        open_clip_torch==3.3.0 timm==1.0.30 huggingface-hub==2.2.0 \
        scikit-learn==1.9.1 pillow==12.3.0 numpy==2.5.2

ARG VERSION="0.0.0-dev"
ARG COMMIT="unknown"
ARG BUILD_DATE="unknown"
LABEL description="Thumbnailer preference-model worker: embeds grids, trains, uploads scores"
LABEL version="${VERSION}"
LABEL commit="${COMMIT}"
LABEL build_date="${BUILD_DATE}"

# The model weights (1.4 GB) are downloaded on first use into /cache: mount a volume there so
# they survive restarts and image updates
RUN useradd --uid 10001 --create-home taste && mkdir /cache && chown taste: /cache
ENV HF_HOME=/cache XDG_CACHE_HOME=/cache PYTHONUNBUFFERED=1
VOLUME /cache

COPY taste.py /app/taste.py
USER taste
WORKDIR /app
ENTRYPOINT ["python", "/app/taste.py"]
CMD ["--help"]
