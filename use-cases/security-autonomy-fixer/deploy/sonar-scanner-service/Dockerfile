ARG SCANNER_IMAGE
FROM ${SCANNER_IMAGE}
COPY source/server.py /opt/e2e/server.py
ENTRYPOINT ["python3", "-u", "/opt/e2e/server.py"]
