FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tanoimg ./cmd/tanoimg

FROM alpine:3.22
RUN addgroup -S tanoimg && adduser -S -G tanoimg tanoimg && mkdir /data && chown tanoimg:tanoimg /data
COPY --from=build /tanoimg /usr/local/bin/tanoimg
USER tanoimg
ENV TANOIMG_DATA=/data TANOIMG_ADDR=:3000
VOLUME /data
EXPOSE 3000
ENTRYPOINT ["tanoimg"]
