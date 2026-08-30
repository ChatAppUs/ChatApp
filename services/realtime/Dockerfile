FROM debian:bookworm-slim AS build
RUN apt-get update && apt-get install -y --no-install-recommends g++ && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY main.cpp sha.h .
RUN g++ -O3 -std=c++17 -Wall -Wextra -o realtime main.cpp

FROM debian:bookworm-slim
COPY --from=build /src/realtime /usr/local/bin/realtime
ENTRYPOINT ["realtime"]
