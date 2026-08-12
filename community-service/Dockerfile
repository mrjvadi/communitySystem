FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.work ./
COPY shared/ shared/
COPY community-service/ community-service/
RUN go build -o /bin/service ./community-service/cmd/...

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bin/service /service
ENTRYPOINT ["/service"]
