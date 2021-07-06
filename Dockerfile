FROM alpine:3.13

RUN set -x \
	&& apk add --no-cache \
		blkid \
		e2fsprogs \
		e2fsprogs-extra \
		util-linux \
		xfsprogs \
		xfsprogs-extra \
	&& mkdir -p /run/docker/plugins

COPY linstor-docker-volume linstor-docker-volume

CMD ["/linstor-docker-volume"]
