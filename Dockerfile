FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /store-doctor ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /store-doctor /store-doctor
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/store-doctor"]
