/*
Copyright 2016 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vsphere_volume

import (
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"k8s.io/kubernetes/pkg/api"
	utiltesting "k8s.io/kubernetes/pkg/util/testing"
	"k8s.io/kubernetes/pkg/volume"
	volumetest "k8s.io/kubernetes/pkg/volume/testing"
)

func TestCanSupport(t *testing.T) {
	tmpDir, err := utiltesting.MkTmpdir("vsphereVolumeTest")
	if err != nil {
		t.Fatalf("can't make a temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	plugMgr := volume.VolumePluginMgr{}
	plugMgr.InitPlugins(ProbeVolumePlugins(), volumetest.NewFakeVolumeHost(tmpDir, nil, nil))

	plug, err := plugMgr.FindPluginByName("kubernetes.io/vsphere_volume")
	if err != nil {
		t.Errorf("Can't find the plugin by name")
	}
	if plug.Name() != "kubernetes.io/vsphere_volume" {
		t.Errorf("Wrong name: %s", plug.Name())
	}

	if !plug.CanSupport(&volume.Spec{Volume: &api.Volume{VolumeSource: api.VolumeSource{VsphereVolume: &api.VsphereVirtualDiskVolumeSource{}}}}) {
		t.Errorf("Expected true")
	}

	if !plug.CanSupport(&volume.Spec{PersistentVolume: &api.PersistentVolume{Spec: api.PersistentVolumeSpec{PersistentVolumeSource: api.PersistentVolumeSource{VsphereVolume: &api.VsphereVirtualDiskVolumeSource{}}}}}) {
		t.Errorf("Expected true")
	}
}

type fakePDManager struct {
	attachDetachDuration time.Duration
}

func getFakeDeviceName(host volume.VolumeHost, pdName string) string {
	return path.Join(host.GetPluginDir(vsphereVolumePluginName), "device", pdName)
}

func (fake *fakePDManager) AttachDisk(b *vsphereVolumeMounter, globalPDPath string) error {
	return nil
}

func (fake *fakePDManager) DetachDisk(c *vsphereVolumeUnmounter) error {
	return nil
}

func (fake *fakePDManager) CreateVolume(c *vsphereVolumeProvisioner) (volumeID string, volumeSizeGB int, err error) {
	return "test-volume-name", 1, nil
}

func (fake *fakePDManager) DeleteVolume(cd *vsphereVolumeDeleter) error {
	if cd.pdName != "test-volume-name" {
		return fmt.Errorf("Deleter got unexpected volume name: %s", cd.pdName)
	}
	return nil
}

func TestPlugin(t *testing.T) {

}
