# Install dependencies with a full Python builder, then ship only the service runtime.
ARG PYTHON_IMAGE=python:3.12-slim-trixie
ARG RUNTIME_IMAGE=gcr.io/distroless/cc-debian13:nonroot
FROM ${PYTHON_IMAGE} AS builder

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /app
COPY pyproject.toml requirements.txt ./
COPY src ./src

RUN apt-get update \
    && apt-get upgrade -y \
    && python -m pip install --no-cache-dir --upgrade pip setuptools wheel \
    && python -m pip install --no-cache-dir . \
    && python -m pip uninstall --yes setuptools wheel pip \
    && mkdir -p /models /state /runtime-libs /runtime-package-status /runtime-licenses \
    && cp -L /usr/lib/*-linux-gnu/libffi.so.8 /runtime-libs/ \
    && cp -L /usr/lib/*-linux-gnu/libbz2.so.1.0 /runtime-libs/ \
    && cp -L /usr/lib/*-linux-gnu/liblzma.so.5 /runtime-libs/ \
    && for package in libffi8 libbz2-1.0 liblzma5; do \
         dpkg-query -s "$package" > "/runtime-package-status/$package"; \
         mkdir -p "/runtime-licenses/$package"; \
         cp -L "/usr/share/doc/$package/copyright" "/runtime-licenses/$package/copyright"; \
       done

FROM ${RUNTIME_IMAGE} AS runtime

LABEL org.opencontainers.image.title="Sub2API Intent Classifier"
LABEL org.opencontainers.image.description="Local ONNX secondary-review inference service"

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PATH=/usr/local/bin:/usr/bin \
    LD_LIBRARY_PATH=/usr/local/lib:/usr/lib \
    HOME=/tmp \
    INTENT_CLASSIFIER_HOST=0.0.0.0 \
    INTENT_CLASSIFIER_PORT=8080 \
    INTENT_CLASSIFIER_MODEL_ROOT=/models \
    INTENT_CLASSIFIER_STATE_DIR=/state

WORKDIR /app
COPY --from=builder /usr/local /usr/local
COPY --from=builder /runtime-libs/ /usr/lib/
# Preserve metadata for every added system library; do not hide packages from scanners.
COPY --from=builder /runtime-package-status/ /var/lib/dpkg/status.d/
COPY --from=builder /runtime-licenses/ /usr/share/doc/
COPY --from=builder --chown=10001:10001 /models /models
COPY --from=builder --chown=10001:10001 /state /state

USER 10001:10001
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/usr/local/bin/python", "-c", "import os,urllib.request; urllib.request.urlopen('http://127.0.0.1:'+os.getenv('INTENT_CLASSIFIER_PORT','8080')+'/health/live', timeout=3).read()"]

ENTRYPOINT ["intent-classifier"]
CMD ["serve"]
