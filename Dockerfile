FROM golang:1.24-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
COPY backend/vendor ./vendor
COPY backend/*.go ./
COPY backend/cmd ./cmd
RUN CGO_ENABLED=0 go build -o /gradebee ./cmd/server

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=build /gradebee /gradebee
EXPOSE 8080
CMD ["/gradebee"]
