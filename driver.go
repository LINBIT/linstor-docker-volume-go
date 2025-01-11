package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	linstor "github.com/LINBIT/golinstor"
	"github.com/LINBIT/golinstor/client"
	"github.com/docker/go-connections/tlsconfig"
	"github.com/docker/go-plugins-helpers/volume"
	"github.com/mitchellh/mapstructure"
	"github.com/rck/unit"
	"github.com/vrischmann/envconfig"
	"gopkg.in/ini.v1"
	"k8s.io/kubernetes/pkg/util/mount"
	mountutils "k8s.io/mount-utils"
	"k8s.io/utils/exec"
)

const (
	datadir         = "data"
	pluginFlagKey   = "Aux/is-linstor-docker-volume"
	pluginFlagValue = "true"
	pluginFSTypeKey = "FileSystem/Type"
)

type LinstorConfig struct {
	Controllers string
	Username    string
	Password    string
	CertFile    string
	KeyFile     string
	CAFile      string
}

type LinstorParams struct {
	Nodes               []string
	ReplicasOnDifferent []string
	ReplicasOnSame      []string
	DisklessStoragePool string
	DoNotPlaceWithRegex string
	FS                  string
	FSOpts              string
	MountOpts           []string
	StoragePool         string
	Size                string
	SizeKiB             uint64
	Replicas            int32
	DisklessOnRemaining bool
}

type LinstorDriver struct {
	config  string
	node    string
	root    string
	mounter *mount.SafeFormatAndMount
	resizer *mountutils.ResizeFs
}

func NewLinstorDriver(config, node, root string) *LinstorDriver {
	return &LinstorDriver{
		config: config,
		node:   node,
		root:   root,
		mounter: &mount.SafeFormatAndMount{
			Interface: mount.New("/bin/mount"),
			Exec:      mount.NewOsExec(),
		},
		resizer: mountutils.NewResizeFs(exec.New()),
	}
}

func (driver *LinstorDriver) newBaseURL(hosts string) (*url.URL, error) {
	scheme := "http"
	host := "localhost:3370"

	if hosts != "" {
		host = strings.Split(hosts, ",")[0]

		if s := strings.Split(host, "://"); len(s) == 2 {
			if s[0] == "linstor+ssl" || s[0] == "https" {
				scheme = "https"
			}

			host = s[1]
		}
	}

	if !strings.Contains(host, ":") {
		switch scheme {
		case "http":
			host += ":3370"
		case "https":
			host += ":3371"
		}
	}

	return url.Parse(scheme + "://" + host)
}

func (driver *LinstorDriver) newClient() (*client.Client, error) {
	config := new(LinstorConfig)

	if err := driver.loadConfig(config); err != nil {
		return nil, err
	}

	err := envconfig.InitWithOptions(config, envconfig.Options{
		Prefix:      "LS",
		AllOptional: true,
	})
	if err != nil {
		return nil, err
	}

	baseURL, err := driver.newBaseURL(config.Controllers)
	if err != nil {
		return nil, err
	}

	tlsConfig, err := tlsconfig.Client(tlsconfig.Options{
		CertFile:           config.CertFile,
		KeyFile:            config.KeyFile,
		CAFile:             config.CAFile,
		InsecureSkipVerify: config.CAFile == "",
		ExclusiveRootPools: true,
	})
	if err != nil {
		return nil, err
	}

	return client.NewClient(
		client.BaseURL(baseURL),
		client.BasicAuth(&client.BasicAuthCfg{
			Username: config.Username,
			Password: config.Password,
		}),
		client.HTTPClient(&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		}),
	)
}

func (driver *LinstorDriver) newParams(options map[string]string) (*LinstorParams, error) {
	params := new(LinstorParams)

	if err := driver.loadConfig(params); err != nil {
		return nil, err
	}

	if options == nil {
		return params, nil
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           params,
		WeaklyTypedInput: true,
		DecodeHook:       mapstructure.StringToSliceHookFunc(" "),
	})
	if err != nil {
		return nil, err
	}

	if err = decoder.Decode(options); err != nil {
		return nil, err
	}

	// convert string Size to SizeKiB
	if params.Size == "" {
		params.Size = "100MB"
	}

	u := unit.MustNewUnit(unit.DefaultUnits)

	strSize := params.Size
	v, err := u.ValueFromString(strSize)
	if err != nil {
		return nil, fmt.Errorf("could not convert '%s' to bytes: %v", strSize, err)
	}

	bytes := v.Value
	lower := 4 * unit.M
	if bytes < lower {
		bytes = lower
	}

	params.SizeKiB = uint64(bytes / unit.K)

	if params.FS == "" {
		params.FS = "ext4"
	}

	if params.Replicas == 0 {
		params.Replicas = 2
	}

	return params, nil
}

func (driver *LinstorDriver) resourcesCreate(ctx context.Context, c *client.Client, req *volume.CreateRequest, params *LinstorParams) error {
	err := c.ResourceDefinitions.CreateVolumeDefinition(ctx, req.Name, client.VolumeDefinitionCreate{
		VolumeDefinition: client.VolumeDefinition{
			SizeKib: params.SizeKiB,
		},
	})
	if err != nil {
		return err
	}

	if len(params.Nodes) == 0 {
		return c.Resources.Autoplace(ctx, req.Name, client.AutoPlaceRequest{
			DisklessOnRemaining: params.DisklessOnRemaining,
			SelectFilter: client.AutoSelectFilter{
				PlaceCount:           params.Replicas,
				StoragePool:          params.StoragePool,
				NotPlaceWithRscRegex: params.DoNotPlaceWithRegex,
				ReplicasOnSame:       params.ReplicasOnSame,
				ReplicasOnDifferent:  params.ReplicasOnDifferent,
			},
		})
	}

	for _, node := range params.Nodes {
		err = c.Resources.Create(ctx, driver.toDiskfullCreate(req.Name, node, params))
		if err != nil {
			return err
		}
	}

	return nil
}

// Create implementation
func (driver *LinstorDriver) Create(req *volume.CreateRequest) error {
	params, err := driver.newParams(req.Options)
	if err != nil {
		return err
	}

	c, err := driver.newClient()
	if err != nil {
		return err
	}

	ctx := context.Background()
	err = c.ResourceDefinitions.Create(ctx, client.ResourceDefinitionCreate{
		ResourceDefinition: client.ResourceDefinition{
			Name: req.Name,
			Props: map[string]string{
				pluginFlagKey:           pluginFlagValue,
				pluginFSTypeKey:         params.FS,
				"FileSystem/MkfsParams": params.FSOpts,
			},
		},
	})
	if err != nil {
		return err
	}

	if err = driver.resourcesCreate(ctx, c, req, params); err != nil {
		return err
	}

	resources, err := c.Resources.GetAll(ctx, req.Name)
	if err != nil {
		return err
	}

	if len(resources) == 0 {
		c.ResourceDefinitions.Delete(ctx, req.Name)
	}

	return nil
}

// Remove implementation
func (driver *LinstorDriver) Remove(req *volume.RemoveRequest) error {
	return driver.remove(req.Name, true)
}

// Mount implementation
func (driver *LinstorDriver) Mount(req *volume.MountRequest) (*volume.MountResponse, error) {
	params, err := driver.newParams(nil)
	if err != nil {
		return nil, err
	}

	c, err := driver.newClient()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	if _, err = c.Resources.Get(ctx, req.Name, driver.node); err == client.NotFoundError {
		err = c.Resources.Create(ctx, driver.toDisklessCreate(req.Name, driver.node, params))
		if err != nil {
			return nil, err
		}
	}

	// properties are not merged, so we have to query the resdef
	// as we set the property there
	resdef, err := c.ResourceDefinitions.Get(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	fstype, ok := resdef.Props[pluginFSTypeKey]
	if !ok {
		return nil, fmt.Errorf("volume '%s' did not contain a file system key", req.Name)
	}

	vol, err := c.Resources.GetVolume(ctx, req.Name, driver.node, 0)
	if err != nil {
		return nil, err
	}

	source := vol.DevicePath
	inUse, err := driver.mounter.DeviceOpened(source)
	if err != nil {
		return nil, err
	}

	if inUse {
		return nil, fmt.Errorf("unable to get exclusive open on %s", source)
	}

	target := driver.realMountPath(req.Name)
	if err = driver.mounter.MakeDir(target); err != nil {
		return nil, err
	}

	err = driver.mounter.Mount(source, target, fstype, params.MountOpts)
	if err != nil {
		return nil, err
	}

	mnt := driver.reportedMountPath(req.Name)
	if _, err = os.Stat(mnt); os.IsNotExist(err) { // check for remount
		if err = driver.mounter.MakeDir(mnt); err != nil {
			return nil, err
		}
	}

	needResize, err := driver.resizer.NeedResize(source, target)
	if err != nil {
		return nil, err
	}

	if needResize {
		if _, err = driver.resizer.Resize(source, target); err != nil {
			return nil, err
		}
	}

	return &volume.MountResponse{Mountpoint: mnt}, nil
}

// Path implementation
func (driver *LinstorDriver) Path(req *volume.PathRequest) (*volume.PathResponse, error) {
	return &volume.PathResponse{Mountpoint: driver.mountPoint(req.Name)}, nil
}

// Unmount implementation
func (driver *LinstorDriver) Unmount(req *volume.UnmountRequest) error {
	target := driver.realMountPath(req.Name)

	notMounted, err := driver.mounter.IsNotMountPoint(target)
	if err != nil || notMounted {
		return err
	}

	if err = driver.mounter.Unmount(target); err != nil {
		return err
	}

	// try to remove now unused dir
	_ = os.Remove(target)

	diskless, err := driver.isDiskless(req.Name)
	// in this case we don't really care about the error, just log it, and keep the diskless assignment.
	if err != nil {
		log.Println(err)
	} else if diskless {
		return driver.remove(req.Name, false)
	}

	return nil
}

// Get implementation
func (driver *LinstorDriver) Get(req *volume.GetRequest) (*volume.GetResponse, error) {
	c, err := driver.newClient()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	resourceDef, err := c.ResourceDefinitions.Get(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	if resourceDef.Props[pluginFlagKey] != pluginFlagValue {
		return nil, fmt.Errorf("volume '%s' is not managed by this plugin", req.Name)
	}

	vol := &volume.Volume{
		Name:       resourceDef.Name,
		Mountpoint: driver.mountPoint(resourceDef.Name),
	}

	return &volume.GetResponse{Volume: vol}, nil
}

// List implementation
func (driver *LinstorDriver) List() (*volume.ListResponse, error) {
	c, err := driver.newClient()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	request := client.RDGetAllRequest{
		Props: []string{pluginFlagKey + "=" + pluginFlagValue},
	}

	resourceDefs, err := c.ResourceDefinitions.GetAll(ctx, request)
	if err != nil {
		return nil, err
	}

	vols := []*volume.Volume{}

	for _, resourceDef := range resourceDefs {
		if resourceDef.Props[pluginFlagKey] != pluginFlagValue {
			continue
		}

		vols = append(vols, &volume.Volume{
			Name:       resourceDef.Name,
			Mountpoint: driver.mountPoint(resourceDef.Name),
		})
	}

	return &volume.ListResponse{Volumes: vols}, nil
}

// Capabilities implementation
func (driver *LinstorDriver) Capabilities() *volume.CapabilitiesResponse {
	return &volume.CapabilitiesResponse{
		Capabilities: volume.Capability{Scope: "global"},
	}
}

func (driver *LinstorDriver) loadConfig(result interface{}) error {
	if _, err := os.Stat(driver.config); os.IsNotExist(err) {
		return nil
	}

	file, err := ini.InsensitiveLoad(driver.config)
	if err != nil {
		return err
	}

	return file.Section("global").MapTo(result)
}

func (driver *LinstorDriver) realMountPath(name string) string {
	return filepath.Join(driver.root, name)
}

func (driver *LinstorDriver) reportedMountPath(name string) string {
	return filepath.Join(driver.realMountPath(name), datadir)
}

func (driver *LinstorDriver) mountPoint(name string) string {
	path := driver.realMountPath(name)
	notMounted, err := driver.mounter.IsNotMountPoint(path)
	if err != nil || notMounted {
		return ""
	}

	return driver.reportedMountPath(name)
}

func (driver *LinstorDriver) toDiskfullCreate(name, node string, params *LinstorParams) client.ResourceCreate {
	props := make(map[string]string)

	if params.StoragePool != "" {
		props[linstor.KeyStorPoolName] = params.StoragePool
	}

	return client.ResourceCreate{
		Resource: client.Resource{
			Name:     name,
			NodeName: node,
			Props:    props,
		},
	}
}

func (driver *LinstorDriver) toDisklessCreate(name, node string, params *LinstorParams) client.ResourceCreate {
	props := make(map[string]string)

	if params.DisklessStoragePool != "" {
		props[linstor.KeyStorPoolName] = params.DisklessStoragePool
	}

	return client.ResourceCreate{
		Resource: client.Resource{
			Name:     name,
			NodeName: node,
			Props:    props,
			Flags:    []string{linstor.FlagDiskless},
		},
	}
}

func (driver *LinstorDriver) isDiskless(name string) (bool, error) {
	lopt := client.ListOpts{Resource: []string{name}, Node: []string{driver.node}}

	c, err := driver.newClient()
	if err != nil {
		return false, err
	}

	ctx := context.Background()

	// view to get storage information as well
	resources, err := c.Resources.GetResourceView(ctx, &lopt)
	if err != nil {
		return false, err
	}

	if len(resources) != 1 {
		return false, errors.New("resource filter has to contain exactly one resource")
	}

	if len(resources[0].Volumes) != 1 {
		return false, errors.New("there has to be exactly one volume in the resource")
	}

	return resources[0].Volumes[0].ProviderKind == client.DISKLESS, nil
}

func (driver *LinstorDriver) remove(name string, global bool) error {
	c, err := driver.newClient()
	if err != nil {
		return err
	}

	ctx := context.Background()

	if !global {
		return c.Resources.Delete(ctx, name, driver.node)
	}

	// global
	snaps, err := c.Resources.GetSnapshots(ctx, name)
	if err != nil {
		return err
	}

	for _, snap := range snaps {
		err = c.Resources.DeleteSnapshot(ctx, name, snap.Name)
		if err != nil {
			return err
		}
	}

	return c.ResourceDefinitions.Delete(ctx, name)
}
