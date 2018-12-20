package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	linstor "github.com/LINBIT/golinstor"
	"github.com/docker/go-plugins-helpers/volume"
)

const (
	NodeListKey            = "nodelist"
	StoragePoolKey         = "storagepool"
	DisklessStoragePoolKey = "disklessstoragepool"
	AutoPlaceKey           = "autoplace"
	DisklessOnRemainingKey = "disklessonremaining"
	SizeKiBKey             = "sizekib"
	EncryptionKey          = "encryption"
	FSTypeKey              = "fstype"
)

type linstorDriver struct {
	controllers string
	mount       string
	node        string
	out         io.Writer
}

func newLinstorDriver(controllers, mount, node string, out io.Writer) *linstorDriver {
	return &linstorDriver{
		controllers: controllers,
		mount:       mount,
		node:        node,
		out:         out,
	}
}

type linstorOpts struct {
	linstor.ResourceDeploymentConfig
	FSType string
}

func (l *linstorDriver) newLinstorOpts(name string, options map[string]string) (*linstorOpts, error) {
	cfg := l.newResourceConfig(name)
	opts := linstorOpts{
		FSType: "ext4",
	}
	clean := strings.NewReplacer("-", "", "_", "")
	for k, v := range options {
		switch strings.ToLower(clean.Replace(k)) {
		case NodeListKey:
			cfg.NodeList = strings.Split(v, " ")
		case StoragePoolKey:
			cfg.StoragePool = v
		case DisklessStoragePoolKey:
			cfg.DisklessStoragePool = v
		case AutoPlaceKey:
			autoplace, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("Unable to parse %s option", k)
			}
			cfg.AutoPlace = autoplace
		case DisklessOnRemainingKey:
			if strings.ToLower(v) == "yes" {
				cfg.DisklessOnRemaining = true
			}
		case SizeKiBKey:
			sizekib, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("Unable to parse %s option", k)
			}
			cfg.SizeKiB = sizekib
		case EncryptionKey:
			if strings.ToLower(v) == "yes" {
				cfg.Encryption = true
			}
		case FSTypeKey:
			opts.FSType = v
		}
	}
	opts.ResourceDeploymentConfig = cfg
	return &opts, nil
}

func (l *linstorDriver) Create(req *volume.CreateRequest) error {
	opts, err := l.newLinstorOpts(req.Name, req.Options)
	if err != nil {
		return err
	}
	resource := linstor.NewResourceDeployment(opts.ResourceDeploymentConfig)
	if err = resource.CreateAndAssign(); err != nil {
		return err
	}
	path, err := resource.WaitForDevPath(l.node, 3)
	if err != nil {
		return err
	}
	mounter := linstor.FSUtil{
		ResourceDeployment: &resource,
		FSType:             opts.FSType,
	}
	return mounter.SafeFormat(path)
}

func (l *linstorDriver) Get(req *volume.GetRequest) (*volume.GetResponse, error) {
	resource := l.newResourceDeployment(req.Name)
	if err := l.resourceMustExist(resource); err != nil {
		return nil, err
	}
	vol := &volume.Volume{
		Name:       resource.Name,
		Mountpoint: l.mountpoint(resource.Name),
	}
	return &volume.GetResponse{vol}, nil
}

func (l *linstorDriver) List() (*volume.ListResponse, error) {
	resource := l.newResourceDeployment("List")
	list, err := resource.ListResourceDefinitions()
	if err != nil {
		return nil, err
	}
	vols := []*volume.Volume{}
	for _, rd := range list {
		vols = append(vols, &volume.Volume{
			Name:       rd.RscName,
			Mountpoint: l.mountpoint(rd.RscName),
		})
	}
	return &volume.ListResponse{Volumes: vols}, nil
}

func (l *linstorDriver) Remove(req *volume.RemoveRequest) error {
	resource := l.newResourceDeployment(req.Name)
	return resource.Delete()
}

func (l *linstorDriver) Path(req *volume.PathRequest) (*volume.PathResponse, error) {
	return &volume.PathResponse{Mountpoint: l.mountpoint(req.Name)}, nil
}

func (l *linstorDriver) Mount(req *volume.MountRequest) (*volume.MountResponse, error) {
	resource := l.newResourceDeployment(req.Name)
	if err := l.resourceMustExist(resource); err != nil {
		return nil, err
	}
	source, err := resource.GetDevPath(l.node, false)
	if err != nil {
		return nil, err
	}
	target := l.mountpath(resource.Name)
	mounter := linstor.FSUtil{
		ResourceDeployment: &resource,
	}
	err = mounter.Mount(source, target)
	if err != nil {
		return nil, err
	}
	return &volume.MountResponse{Mountpoint: target}, err
}

func (l *linstorDriver) Unmount(req *volume.UnmountRequest) error {
	resource := l.newResourceDeployment(req.Name)
	if err := l.resourceMustExist(resource); err != nil {
		return err
	}
	path := l.mountpath(req.Name)
	mounter := linstor.FSUtil{
		ResourceDeployment: &resource,
	}
	return mounter.UnMount(path)
}

func (l *linstorDriver) Capabilities() *volume.CapabilitiesResponse {
	return &volume.CapabilitiesResponse{
		Capabilities: volume.Capability{Scope: "global"},
	}
}

func (l *linstorDriver) newResourceConfig(name string) linstor.ResourceDeploymentConfig {
	return linstor.ResourceDeploymentConfig{
		Name:        name,
		Controllers: l.controllers,
		LogOut:      l.out,
	}
}

func (l *linstorDriver) newResourceDeployment(name string) linstor.ResourceDeployment {
	cfg := l.newResourceConfig(name)
	return linstor.NewResourceDeployment(cfg)
}

func (l *linstorDriver) resourceMustExist(resource linstor.ResourceDeployment) error {
	exists, err := resource.Exists()
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("Can't find volume")
	}
	return nil
}

func (l *linstorDriver) mountpath(name string) string {
	return filepath.Join(l.mount, name)
}

func (l *linstorDriver) mountpoint(name string) string {
	path := l.mountpath(name)
	if l.isMounted(path) {
		return path
	}
	return ""
}

func (l *linstorDriver) isMounted(path string) bool {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 1 && fields[1] == path {
			return true
		}
	}
	return false
}
