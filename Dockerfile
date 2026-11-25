FROM golang:1.24 AS builder
WORKDIR /src
ENV GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/cdp .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/cdp /usr/local/bin/cdp
COPY config.example.yaml /etc/cdp/config.example.yaml
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/cdp"]
CMD ["approve", "-config", "/etc/cdp/config.yaml"]
