FROM dagger/dagger:latest

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

COPY dagger /dagger

ENTRYPOINT ["/entrypoint.sh"]