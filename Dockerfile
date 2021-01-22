FROM debian:stretch

RUN set -x \
  && apt-get update \
  && apt-get install -y --no-install-recommends apt-transport-https ca-certificates curl bzip2

COPY node-watcher /bin/
RUN set -x \
  && chmod +x /bin/node-watcher

ENTRYPOINT ["node-watcher"]
