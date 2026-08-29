FROM debian:bookworm-slim AS build
RUN apt-get update && apt-get install -y --no-install-recommends g++ && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY main.cpp .
RUN g++ -O3 -std=c++17 -pthread -o /bin/media main.cpp

FROM debian:bookworm-slim
RUN useradd -m app && mkdir -p /data/uploads && chown app /data/uploads
USER app
COPY --from=build /bin/media /bin/media
EXPOSE 8100
ENTRYPOINT ["/bin/media"]
