package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/docker/go-plugins-helpers/volume"
)

const linstorID = "linstor"

var (
	controllers = os.Getenv("CONTROLLERS")
	mount       = filepath.Join(volume.DefaultDockerRootDirectory, linstorID)
	out         = os.Stdout
)

func main() {
	log.SetOutput(out)
	node, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}

	driver := newLinstorDriver(controllers, mount, node, out)
	handler := volume.NewHandler(driver)
	err = handler.ServeUnix(linstorID, 0)
	if err != nil {
		log.Fatal(err)
	}
}
