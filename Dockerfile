FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY backend/dist/gradebee /gradebee
EXPOSE 8080
CMD ["/gradebee"]
