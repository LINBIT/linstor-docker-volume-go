# Linstor docker volume plugin

## Building

Requires Docker 1.13.0 or higher

`git clone https://github.com/beornf/linstor-docker-volume`

`cd linstor-docker-volume`

`make`

## Installing

`docker plugin install lade/linstor`

## Configuration

```
cat /etc/linstor/docker-volume.conf
[global]
controllers = linstor://hostnameofcontroller
fs = xfs
```

## License
GPL2
