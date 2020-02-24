# Linstor docker volume plugin

Development of this plugin is coordinated on
[beornf/linstor-docker-volume](https://github.com/beornf/linstor-docker-volume). This is the modern version of
a [Python based plugin](https://github.com/LINBIT/linstor-docker-volume) developed by LINBIT. Thanks to [Beorn
Facchini](https://github.com/beornf) for creating a modern Go based incarnation.

Consider this a mirror under control of LINBIT developers.

## Building

Requires Docker 1.13.0 or higher

`git clone https://github.com/LINBIT/linstor-docker-volume-go`

`cd linstor-docker-volume-go`

`make PLUGIN_NAME=linbit/linstor-docker-volume`

## Installing

`docker plugin install linbit/linstor-docker-volume`

## Configuration

```
cat /etc/linstor/docker-volume.conf
[global]
controllers = linstor://hostnameofcontroller
fs = xfs
```

## License
GPL2
