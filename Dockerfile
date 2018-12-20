FROM alpine:3.8

ENV LINSTOR_VERSION 0.7.3

RUN set -ex \
	&& apk add e2fsprogs make protobuf py-setuptools util-linux xfsprogs \
	&& wget https://www.linbit.com/downloads/linstor/python-linstor-${LINSTOR_VERSION}.tar.gz \
	&& tar -xzf python-linstor-${LINSTOR_VERSION}.tar.gz && cd python-linstor-${LINSTOR_VERSION} \
	&& make install && cd .. && rm -r python-linstor* \
	&& wget https://www.linbit.com/downloads/linstor/linstor-client-${LINSTOR_VERSION}.tar.gz \
	&& tar -xzf linstor-client-${LINSTOR_VERSION}.tar.gz && cd linstor-client-${LINSTOR_VERSION} \
	&& make install && cd .. && rm -r linstor-client* \
	&& apk del make protobuf && rm -rf /var/cache/apk/* \
	&& mkdir -p /run/docker/plugins

COPY linstor-docker-volume linstor-docker-volume

CMD ["/linstor-docker-volume"]
