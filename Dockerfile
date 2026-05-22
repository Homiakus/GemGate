FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gemgate ./cmd/gemgate

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/gemgate /app/gemgate
COPY config.example.yaml /app/config.yaml
EXPOSE 8080
ENTRYPOINT ["/app/gemgate"]
CMD ["serve", "-config", "/app/config.yaml"]
