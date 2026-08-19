# 兩階段建置：編出靜態 binary，跑在 alpine 上
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/oauth-proxy .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/oauth-proxy /usr/local/bin/oauth-proxy
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/oauth-proxy"]
