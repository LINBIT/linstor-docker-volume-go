package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/docker/go-plugins-helpers/volume"
)

const (
	config = "/etc/linstor/docker-volume.conf"
	plugin = "linstor"
)

var (
	root = filepath.Join(volume.DefaultDockerRootDirectory, plugin)
)

func init() {
	log.SetOutput(ioutil.Discard)
}

func main() {
	node, err := os.Hostname()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	driver := NewLinstorDriver(config, node, root)
	handler := volume.NewHandler(driver)
	fmt.Println(handler.ServeUnix(plugin, 0))
}
