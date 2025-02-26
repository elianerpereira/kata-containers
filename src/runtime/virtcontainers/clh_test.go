// Copyright (c) 2019 Ericsson Eurolab Deutschland G.m.b.H.
//
// SPDX-License-Identifier: Apache-2.0
//

package virtcontainers

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/device/config"
	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/persist"
	chclient "github.com/kata-containers/kata-containers/src/runtime/virtcontainers/pkg/cloud-hypervisor/client"
	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/types"
	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/utils"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

const (
	FAIL = true
	PASS = !FAIL
)

func newClhConfig() (HypervisorConfig, error) {

	setupClh()

	if testClhPath == "" {
		return HypervisorConfig{}, errors.New("hypervisor fake path is empty")
	}

	if testVirtiofsdPath == "" {
		return HypervisorConfig{}, errors.New("virtiofsd fake path is empty")
	}

	if _, err := os.Stat(testClhPath); os.IsNotExist(err) {
		return HypervisorConfig{}, err
	}

	if _, err := os.Stat(testVirtiofsdPath); os.IsNotExist(err) {
		return HypervisorConfig{}, err
	}

	return HypervisorConfig{
		KernelPath:        testClhKernelPath,
		ImagePath:         testClhImagePath,
		HypervisorPath:    testClhPath,
		NumVCPUs:          defaultVCPUs,
		BlockDeviceDriver: config.VirtioBlock,
		MemorySize:        defaultMemSzMiB,
		DefaultBridges:    defaultBridges,
		DefaultMaxVCPUs:   uint32(64),
		SharedFS:          config.VirtioFS,
		VirtioFSCache:     virtioFsCacheAlways,
		VirtioFSDaemon:    testVirtiofsdPath,
	}, nil
}

type clhClientMock struct {
	vmInfo chclient.VmInfo
}

func (c *clhClientMock) VmmPingGet(ctx context.Context) (chclient.VmmPingResponse, *http.Response, error) {
	return chclient.VmmPingResponse{}, nil, nil
}

func (c *clhClientMock) ShutdownVMM(ctx context.Context) (*http.Response, error) {
	return nil, nil
}

func (c *clhClientMock) CreateVM(ctx context.Context, vmConfig chclient.VmConfig) (*http.Response, error) {
	c.vmInfo.State = clhStateCreated
	return nil, nil
}

//nolint:golint
func (c *clhClientMock) VmInfoGet(ctx context.Context) (chclient.VmInfo, *http.Response, error) {
	return c.vmInfo, nil, nil
}

func (c *clhClientMock) BootVM(ctx context.Context) (*http.Response, error) {
	c.vmInfo.State = clhStateRunning
	return nil, nil
}

//nolint:golint
func (c *clhClientMock) VmResizePut(ctx context.Context, vmResize chclient.VmResize) (*http.Response, error) {
	return nil, nil
}

//nolint:golint
func (c *clhClientMock) VmAddDevicePut(ctx context.Context, vmAddDevice chclient.VmAddDevice) (chclient.PciDeviceInfo, *http.Response, error) {
	return chclient.PciDeviceInfo{}, nil, nil
}

//nolint:golint
func (c *clhClientMock) VmAddDiskPut(ctx context.Context, diskConfig chclient.DiskConfig) (chclient.PciDeviceInfo, *http.Response, error) {
	return chclient.PciDeviceInfo{Bdf: "0000:00:0a.0"}, nil, nil
}

//nolint:golint
func (c *clhClientMock) VmRemoveDevicePut(ctx context.Context, vmRemoveDevice chclient.VmRemoveDevice) (*http.Response, error) {
	return nil, nil
}

func TestCloudHypervisorAddVSock(t *testing.T) {
	assert := assert.New(t)
	clh := cloudHypervisor{}

	clh.addVSock(1, "path")
	assert.Equal(clh.vmconfig.Vsock.Cid, int64(1))
	assert.Equal(clh.vmconfig.Vsock.Socket, "path")
}

// Check addNet appends to the network config list new configurations.
// Check that the elements in the list has the correct values
func TestCloudHypervisorAddNetCheckNetConfigListValues(t *testing.T) {
	macTest := "00:00:00:00:00"
	tapPath := "/path/to/tap"

	assert := assert.New(t)

	clh := cloudHypervisor{}

	e := &VethEndpoint{}
	e.NetPair.TAPIface.HardAddr = macTest
	e.NetPair.TapInterface.TAPIface.Name = tapPath

	err := clh.addNet(e)
	assert.Nil(err)

	assert.Equal(len(*clh.vmconfig.Net), 1)
	if err == nil {
		assert.Equal(*(*clh.vmconfig.Net)[0].Mac, macTest)
		assert.Equal(*(*clh.vmconfig.Net)[0].Tap, tapPath)
	}

	err = clh.addNet(e)
	assert.Nil(err)

	assert.Equal(len(*clh.vmconfig.Net), 2)
	if err == nil {
		assert.Equal(*(*clh.vmconfig.Net)[1].Mac, macTest)
		assert.Equal(*(*clh.vmconfig.Net)[1].Tap, tapPath)
	}
}

// Check addNet with valid values, and fail with invalid values
// For Cloud Hypervisor only tap is be required
func TestCloudHypervisorAddNetCheckEnpointTypes(t *testing.T) {
	assert := assert.New(t)

	tapPath := "/path/to/tap"

	validVeth := &VethEndpoint{}
	validVeth.NetPair.TapInterface.TAPIface.Name = tapPath

	type args struct {
		e Endpoint
	}
	// nolint: govet
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"TapEndpoint", args{e: &TapEndpoint{}}, true},
		{"Empty VethEndpoint", args{e: &VethEndpoint{}}, true},
		{"Valid VethEndpoint", args{e: validVeth}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clh := &cloudHypervisor{}
			if err := clh.addNet(tt.args.e); (err != nil) != tt.wantErr {
				t.Errorf("cloudHypervisor.addNet() error = %v, wantErr %v", err, tt.wantErr)

			} else if err == nil {
				assert.Equal(*(*clh.vmconfig.Net)[0].Tap, tapPath)
			}
		})
	}
}

func TestCloudHypervisorBootVM(t *testing.T) {
	clh := &cloudHypervisor{}
	clh.APIClient = &clhClientMock{}
	var ctx context.Context
	if err := clh.bootVM(ctx); err != nil {
		t.Errorf("cloudHypervisor.bootVM() error = %v", err)
	}
}

func TestCloudHypervisorCleanupVM(t *testing.T) {
	assert := assert.New(t)
	store, err := persist.GetDriver()
	assert.NoError(err, "persist.GetDriver() unexpected error")

	clh := &cloudHypervisor{
		config: HypervisorConfig{
			VMStorePath:  store.RunVMStoragePath(),
			RunStorePath: store.RunStoragePath(),
		},
	}

	err = clh.cleanupVM(true)
	assert.Error(err, "persist.GetDriver() expected error")

	clh.id = "cleanVMID"

	err = clh.cleanupVM(true)
	assert.NoError(err, "persist.GetDriver() unexpected error")

	dir := filepath.Join(store.RunVMStoragePath(), clh.id)
	os.MkdirAll(dir, os.ModePerm)

	err = clh.cleanupVM(false)
	assert.NoError(err, "persist.GetDriver() unexpected error")

	_, err = os.Stat(dir)
	assert.Error(err, "dir should not exist %s", dir)

	assert.True(os.IsNotExist(err), "persist.GetDriver() unexpected error")
}

func TestClhCreateVMWithInitrd(t *testing.T) {
	assert := assert.New(t)

	clhConfig, err := newClhConfig()
	assert.NoError(err)
	clhConfig.ImagePath = ""
	clhConfig.InitrdPath = testClhInitrdPath

	store, err := persist.GetDriver()
	assert.NoError(err)

	clhConfig.VMStorePath = store.RunVMStoragePath()
	clhConfig.RunStorePath = store.RunStoragePath()

	clh := &cloudHypervisor{
		config: clhConfig,
	}

	sandbox := &Sandbox{
		ctx: context.Background(),
		id:  "testSandbox",
		config: &SandboxConfig{
			HypervisorConfig: clhConfig,
		},
	}

	err = clh.CreateVM(context.Background(), sandbox.id, NetworkNamespace{}, &sandbox.config.HypervisorConfig)
	assert.NoError(err)
	assert.Exactly(clhConfig, clh.config)
}

func TestClhCreateVM(t *testing.T) {
	assert := assert.New(t)

	clhConfig, err := newClhConfig()
	assert.NoError(err)
	assert.NotEmpty(clhConfig.ImagePath)

	store, err := persist.GetDriver()
	assert.NoError(err)

	clhConfig.VMStorePath = store.RunVMStoragePath()
	clhConfig.RunStorePath = store.RunStoragePath()

	clh := &cloudHypervisor{
		config: clhConfig,
	}

	sandbox := &Sandbox{
		ctx: context.Background(),
		id:  "testSandbox",
		config: &SandboxConfig{
			HypervisorConfig: clhConfig,
		},
	}

	err = clh.CreateVM(context.Background(), sandbox.id, NetworkNamespace{}, &sandbox.config.HypervisorConfig)
	assert.NoError(err)
	assert.Exactly(clhConfig, clh.config)
}

func TestClooudHypervisorStartSandbox(t *testing.T) {
	assert := assert.New(t)
	clhConfig, err := newClhConfig()
	assert.NoError(err)

	store, err := persist.GetDriver()
	assert.NoError(err)

	clhConfig.VMStorePath = store.RunVMStoragePath()
	clhConfig.RunStorePath = store.RunStoragePath()

	clh := &cloudHypervisor{
		config:    clhConfig,
		APIClient: &clhClientMock{},
		virtiofsd: &virtiofsdMock{},
	}

	err = clh.StartVM(context.Background(), 10)
	assert.NoError(err)
}

func TestCloudHypervisorResizeMemory(t *testing.T) {
	assert := assert.New(t)
	clhConfig, err := newClhConfig()
	type args struct {
		reqMemMB          uint32
		memoryBlockSizeMB uint32
	}
	tests := []struct {
		name           string
		args           args
		expectedMemDev MemoryDevice
		wantErr        bool
	}{
		{"Resize to zero", args{0, 128}, MemoryDevice{Probe: false, SizeMB: 0}, FAIL},
		{"Resize to aligned size", args{clhConfig.MemorySize + 128, 128}, MemoryDevice{Probe: false, SizeMB: 128}, PASS},
		{"Resize to aligned size", args{clhConfig.MemorySize + 129, 128}, MemoryDevice{Probe: false, SizeMB: 256}, PASS},
		{"Resize to NOT aligned size", args{clhConfig.MemorySize + 125, 128}, MemoryDevice{Probe: false, SizeMB: 128}, PASS},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(err)
			clh := cloudHypervisor{}

			mockClient := &clhClientMock{}
			mockClient.vmInfo.Config = *chclient.NewVmConfig(*chclient.NewKernelConfig(""))
			mockClient.vmInfo.Config.Memory = chclient.NewMemoryConfig(int64(utils.MemUnit(clhConfig.MemorySize) * utils.MiB))
			mockClient.vmInfo.Config.Memory.HotplugSize = func(i int64) *int64 { return &i }(int64(40 * utils.GiB.ToBytes()))

			clh.APIClient = mockClient
			clh.config = clhConfig

			newMem, memDev, err := clh.ResizeMemory(context.Background(), tt.args.reqMemMB, tt.args.memoryBlockSizeMB, false)

			if (err != nil) != tt.wantErr {
				t.Errorf("cloudHypervisor.ResizeMemory() error = %v, expected to fail = %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			expectedMem := clhConfig.MemorySize + uint32(tt.expectedMemDev.SizeMB)

			if newMem != expectedMem {
				t.Errorf("cloudHypervisor.ResizeMemory() got = %+v, want %+v", newMem, expectedMem)
			}

			if !reflect.DeepEqual(memDev, tt.expectedMemDev) {
				t.Errorf("cloudHypervisor.ResizeMemory() got = %+v, want %+v", memDev, tt.expectedMemDev)
			}
		})
	}
}

func TestCloudHypervisorHotplugAddBlockDevice(t *testing.T) {
	assert := assert.New(t)

	clhConfig, err := newClhConfig()
	assert.NoError(err)

	clh := &cloudHypervisor{}
	clh.config = clhConfig
	clh.APIClient = &clhClientMock{}

	clh.config.BlockDeviceDriver = config.VirtioBlock
	err = clh.hotplugAddBlockDevice(&config.BlockDrive{Pmem: false})
	assert.NoError(err, "Hotplug disk block device expected no error")

	err = clh.hotplugAddBlockDevice(&config.BlockDrive{Pmem: true})
	assert.Error(err, "Hotplug pmem block device expected error")

	clh.config.BlockDeviceDriver = config.VirtioSCSI
	err = clh.hotplugAddBlockDevice(&config.BlockDrive{Pmem: false})
	assert.Error(err, "Hotplug block device not using 'virtio-blk' expected error")
}

func TestCloudHypervisorHotplugRemoveDevice(t *testing.T) {
	assert := assert.New(t)

	clhConfig, err := newClhConfig()
	assert.NoError(err)

	clh := &cloudHypervisor{}
	clh.config = clhConfig
	clh.APIClient = &clhClientMock{}

	_, err = clh.HotplugRemoveDevice(context.Background(), &config.BlockDrive{}, BlockDev)
	assert.NoError(err, "Hotplug remove block device expected no error")

	_, err = clh.HotplugRemoveDevice(context.Background(), &config.VFIODev{}, VfioDev)
	assert.NoError(err, "Hotplug remove vfio block device expected no error")

	_, err = clh.HotplugRemoveDevice(context.Background(), nil, NetDev)
	assert.Error(err, "Hotplug remove pmem block device expected error")
}

func TestClhGenerateSocket(t *testing.T) {
	assert := assert.New(t)

	// Ensure the type is fully constructed
	hypervisor, err := NewHypervisor("clh")
	assert.NoError(err)

	clh, ok := hypervisor.(*cloudHypervisor)
	assert.True(ok)

	clh.config = HypervisorConfig{
		VMStorePath:  "/foo",
		RunStorePath: "/bar",
	}

	clh.addVSock(1, "path")

	s, err := clh.GenerateSocket("c")

	assert.NoError(err)
	assert.NotNil(s)

	hvsock, ok := s.(types.HybridVSock)
	assert.True(ok)
	assert.NotEmpty(hvsock.UdsPath)

	// Path must be absolute
	assert.True(strings.HasPrefix(hvsock.UdsPath, "/"), "failed: socket path: %s", hvsock.UdsPath)

	assert.NotZero(hvsock.Port)
}

func TestClhSetConfig(t *testing.T) {
	assert := assert.New(t)

	config, err := newClhConfig()
	assert.NoError(err)

	clh := &cloudHypervisor{}
	assert.Equal(clh.config, HypervisorConfig{})

	err = clh.setConfig(&config)
	assert.NoError(err)

	assert.Equal(clh.config, config)
}
