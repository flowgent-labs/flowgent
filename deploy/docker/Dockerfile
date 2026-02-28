# Flowgent — update binary on top of existing base image
FROM localhost/flowgent/jobmanager:latest
COPY bin/flowgent /app/flowgent
# CA certificates for TLS verification (DeepSeek API etc.)
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
