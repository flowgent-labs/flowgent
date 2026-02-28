FROM registry.cn-shenzhen.aliyuncs.com/wl4g/alpine:3.21
COPY flowgent /app/flowgent
COPY github-mcp /bin/github-mcp
COPY sonarqube-mcp /bin/sonarqube-mcp
COPY mcp-server-sonatypeiq /bin/sonatype-iq-mcp
COPY mcp-server-nexus3 /bin/nexus3-mcp
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
