FROM alpine:3.21

RUN apk add --no-cache build-base curl

COPY run.sh /run.sh
RUN chmod +x /run.sh

USER nobody
ENTRYPOINT ["/run.sh"]
