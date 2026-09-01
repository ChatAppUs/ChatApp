FROM debian:bookworm-slim AS build
RUN apt-get update && apt-get install -y --no-install-recommends g++ && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY main.cpp .
RUN g++ -O3 -std=c++17 -Wall -Wextra -pthread -o counters main.cpp

FROM debian:bookworm-slim
COPY --from=build /src/counters /usr/local/bin/counters
ENTRYPOINT ["counters"]
