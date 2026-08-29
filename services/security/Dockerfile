FROM rust:1.85-alpine AS build
WORKDIR /src
COPY Cargo.toml ./
COPY src ./src
RUN cargo build --release

FROM alpine:3.20
RUN adduser -D app
USER app
COPY --from=build /src/target/release/chatapp-security /bin/security
EXPOSE 8090
ENTRYPOINT ["/bin/security"]
