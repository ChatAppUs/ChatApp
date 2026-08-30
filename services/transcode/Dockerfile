FROM debian:bookworm-slim AS build
RUN apt-get update && apt-get install -y --no-install-recommends g++ && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY main.cpp .
RUN g++ -O3 -std=c++17 -Wall -Wextra -o transcode main.cpp

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg && rm -rf /var/lib/apt/lists/*
COPY --from=build /src/transcode /usr/local/bin/transcode
ENTRYPOINT ["transcode"]
