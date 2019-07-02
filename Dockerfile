FROM debian:stretch-slim

ENV DEBIAN_FRONTEND=noninteractive \
	DEB_LIST='/etc/apt/sources.list.d' \
	PKG_DEP='curl gnupg'

RUN set -x \
	&& apt-get update && apt-get install $PKG_DEP xfsprogs -y \
	&& echo 'deb http://mirror.lade.io/debian stretch main' > $DEB_LIST/lade.list \
	&& curl -sSL http://mirror.lade.io/lade.gpg | apt-key add - && apt-get update \
	&& apt-get install --no-install-recommends linstor-client -y \
	&& apt-get autoremove --purge $PKG_DEP -y && rm -rf /var/lib/apt/lists/* \
	&& mkdir -p /run/docker/plugins

COPY linstor-docker-volume linstor-docker-volume

CMD ["/linstor-docker-volume"]
