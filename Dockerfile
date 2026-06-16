FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/chronos-wallet ./cmd/server

FROM alpine:3.22
COPY --from=build /out/chronos-wallet /usr/local/bin/chronos-wallet
EXPOSE 8080
ENTRYPOINT ["chronos-wallet"]
