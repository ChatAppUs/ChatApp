FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/sfu .

FROM alpine:3.20
RUN adduser -D app && apk add --no-cache ca-certificates
USER app
COPY --from=build /bin/sfu /bin/sfu
EXPOSE 8095 3478/udp
ENTRYPOINT ["/bin/sfu"]
