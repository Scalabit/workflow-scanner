FROM ubuntu:22.04

RUN apt-get update && apt-get install -y \
    curl \
    ca-certificates \
    bash \
    && rm -rf /var/lib/apt/lists/*

RUN cd /usr/local && \
    curl -L https://dl.dagger.io/dagger/install.sh | sh

ENV PATH="/usr/local/bin:${PATH}"

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

COPY dagger /dagger

ENTRYPOINT ["/entrypoint.sh"]