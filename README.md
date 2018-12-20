# Linstor docker volume plugin

## Building

Requires Docker 1.13.0 or higher

`git clone https://github.com/beornf/linstor-docker-volume`

`cd linstor-docker-volume`

`make`

## Installing

`docker plugin install lade/linstor CONTROLLERS=*CONTROLLERS*`

## Options

`--opt node-list=*NODES*`

`--opt storage-pool=*POOL*`

`--opt diskless-storage-pool=*POOL*`

`--opt auto-place=*COUNT*`

`--opt diskless-on-remaining=[yes/no]`

`--opt size-kib=*SIZE*`

`--opt encryption=[yes/no]`

`--opt fs-type=*TYPE*`

## License
GPL2
