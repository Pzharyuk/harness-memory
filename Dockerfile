FROM golang:1.24 AS build

WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/memoryd ./cmd/memoryd

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/memoryd /memoryd
USER nonroot:nonroot
ENTRYPOINT ["/memoryd"]
