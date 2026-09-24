# Build the Linux binary before building this benchmark-only runtime image.
# Match the production Dockerfile's runtime and user; avoid rebuilding frontend.
FROM alpine:3.22
RUN addgroup -S tanoimg && adduser -S -G tanoimg tanoimg && mkdir /data && chown tanoimg:tanoimg /data
COPY tanoimg-perf-linux /usr/local/bin/tanoimg
USER tanoimg
ENV TANOIMG_DATA=/data TANOIMG_ADDR=:3000
EXPOSE 3000
ENTRYPOINT ["tanoimg"]
