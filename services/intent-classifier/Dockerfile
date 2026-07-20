ARG PYTHON_IMAGE=python:3.12-slim-bookworm
FROM ${PYTHON_IMAGE}

LABEL org.opencontainers.image.title="Sub2API Intent Classifier"
LABEL org.opencontainers.image.description="Local ONNX secondary-review inference service"

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    INTENT_CLASSIFIER_HOST=0.0.0.0 \
    INTENT_CLASSIFIER_PORT=8080 \
    INTENT_CLASSIFIER_MODEL_ROOT=/models \
    INTENT_CLASSIFIER_STATE_DIR=/state

WORKDIR /app

RUN groupadd --gid 10001 classifier \
    && useradd --uid 10001 --gid classifier --no-create-home --shell /usr/sbin/nologin classifier

COPY pyproject.toml requirements.txt ./
COPY src ./src

RUN python -m pip install --no-cache-dir . \
    && mkdir -p /models /state \
    && chown -R classifier:classifier /app /state

USER 10001:10001

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD python -c "import os,urllib.request; urllib.request.urlopen('http://127.0.0.1:'+os.getenv('INTENT_CLASSIFIER_PORT','8080')+'/health/live', timeout=3).read()" || exit 1

ENTRYPOINT ["intent-classifier"]
CMD ["serve"]
