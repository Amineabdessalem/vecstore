FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /vecstore ./cmd/vecstore

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /vecstore /vecstore
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/vecstore", "-addr", ":8080", "-data", "/data"]
