FROM alpine:latest
RUN apk add --no-cache ca-certificates poppler-utils
COPY backend/dist/gradebee /gradebee
EXPOSE 8080
CMD ["/gradebee"]
