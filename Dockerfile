FROM golang:1.25-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -o /out/worker ./cmd/worker

FROM alpine:3.20
COPY --from=build /out/api /usr/local/bin/order-hub-api
COPY --from=build /out/worker /usr/local/bin/order-hub-worker
EXPOSE 8080
CMD ["order-hub-api"]
