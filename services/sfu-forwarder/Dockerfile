FROM debian:bookworm-slim AS build
RUN apt-get update && apt-get install -y --no-install-recommends g++ && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY main.cpp .
RUN g++ -O3 -std=c++17 -Wall -Wextra -Werror -pthread -o sfu-forwarder main.cpp

FROM debian:bookworm-slim
RUN useradd --system --no-create-home app
USER app
COPY --from=build /src/sfu-forwarder /usr/local/bin/sfu-forwarder
EXPOSE 3479/tcp 3479/udp 8099
ENTRYPOINT ["/usr/local/bin/sfu-forwarder"]
