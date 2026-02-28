FROM registry.cn-shenzhen.aliyuncs.com/wl4g/alpine:3.21
COPY bin/flowgent /app/flowgent
COPY bin/github-mcp /bin/github-mcp
COPY bin/sonarqube-mcp /bin/sonarqube-mcp
COPY bin/mcp-server-sonatypeiq /bin/sonatype-iq-mcp
COPY bin/mcp-server-nexus3 /bin/nexus3-mcp
COPY bin/mcp-server-test /bin/test-mcp
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
