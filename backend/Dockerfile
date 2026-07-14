FROM golang:1.23-alpine AS backend-build

WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget \
	&& addgroup -S aiops \
	&& adduser -S -G aiops aiops

WORKDIR /app
COPY --from=backend-build /out/server /app/server
COPY --from=backend-build /out/migrate /app/migrate

RUN mkdir -p /app/data/uploads \
	&& chown -R aiops:aiops /app

USER aiops
ENV APP_PORT=8080
ENV LOCAL_FILE_DIR=/app/data/uploads

EXPOSE 8080
ENTRYPOINT ["/app/server"]
