/*
Copyright 2017 The Kubernetes Authors.
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

package util

import (
	"fmt"

	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/api"
	kcache "k8s.io/kubernetes/pkg/client/cache"
	"k8s.io/kubernetes/pkg/controller/volume/attachdetach/cache"
	"k8s.io/kubernetes/pkg/types"
	"k8s.io/kubernetes/pkg/volume"
	"k8s.io/kubernetes/pkg/volume/util/volumehelper"
)

// CreateVolumeSpec creates and returns a mutatable volume.Spec object for the
// specified volume. It dereference any PVC to get PV objects, if needed.
func CreateVolumeSpec(podVolume api.Volume, podNamespace string, pvcInformer kcache.SharedInformer, pvInformer kcache.SharedInformer) (*volume.Spec, error) {
	if pvcSource := podVolume.VolumeSource.PersistentVolumeClaim; pvcSource != nil {
		glog.V(10).Infof(
			"Found PVC, ClaimName: %q/%q",
			podNamespace,
			pvcSource.ClaimName)

		// If podVolume is a PVC, fetch the real PV behind the claim
		pvName, pvcUID, err := getPVCFromCacheExtractPV(
			podNamespace, pvcSource.ClaimName, pvcInformer)
		if err != nil {
			return nil, fmt.Errorf(
				"error processing PVC %q/%q: %v",
				podNamespace,
				pvcSource.ClaimName,
				err)
		}

		glog.V(10).Infof(
			"Found bound PV for PVC (ClaimName %q/%q pvcUID %v): pvName=%q",
			podNamespace,
			pvcSource.ClaimName,
			pvcUID,
			pvName)

		// Fetch actual PV object
		volumeSpec, err := getPVSpecFromCache(
			pvName, pvcSource.ReadOnly, pvcUID, pvInformer)
		if err != nil {
			return nil, fmt.Errorf(
				"error processing PVC %q/%q: %v",
				podNamespace,
				pvcSource.ClaimName,
				err)
		}

		glog.V(10).Infof(
			"Extracted volumeSpec (%v) from bound PV (pvName %q) and PVC (ClaimName %q/%q pvcUID %v)",
			volumeSpec.Name,
			pvName,
			podNamespace,
			pvcSource.ClaimName,
			pvcUID)

		return volumeSpec, nil
	}

	// Do not return the original volume object, since it's from the shared
	// informer it may be mutated by another consumer.
	clonedPodVolumeObj, err := api.Scheme.DeepCopy(&podVolume)
	if err != nil || clonedPodVolumeObj == nil {
		return nil, fmt.Errorf(
			"failed to deep copy %q volume object. err=%v", podVolume.Name, err)
	}

	clonedPodVolume, ok := clonedPodVolumeObj.(*api.Volume)
	if !ok {
		return nil, fmt.Errorf("failed to cast clonedPodVolume %#v to api.Volume", clonedPodVolumeObj)
	}

	return volume.NewSpecFromVolume(clonedPodVolume), nil
}

// getPVCFromCacheExtractPV fetches the PVC object with the given namespace and
// name from the shared internal PVC store extracts the name of the PV it is
// pointing to and returns it.
// This method returns an error if a PVC object does not exist in the cache
// with the given namespace/name.
// This method returns an error if the PVC object's phase is not "Bound".
func getPVCFromCacheExtractPV(namespace string, name string, pvcInformer kcache.SharedInformer) (string, types.UID, error) {
	key := name
	if len(namespace) > 0 {
		key = namespace + "/" + name
	}

	pvcObj, exists, err := pvcInformer.GetStore().GetByKey(key)
	if pvcObj == nil || !exists || err != nil {
		return "", "", fmt.Errorf(
			"failed to find PVC %q in PVCInformer cache. %v",
			key,
			err)
	}

	pvc, ok := pvcObj.(*api.PersistentVolumeClaim)
	if !ok || pvc == nil {
		return "", "", fmt.Errorf(
			"failed to cast %q object %#v to PersistentVolumeClaim",
			key,
			pvcObj)
	}

	if pvc.Status.Phase != api.ClaimBound || pvc.Spec.VolumeName == "" {
		return "", "", fmt.Errorf(
			"PVC %s/%s has non-bound phase (%q) or empty pvc.Spec.VolumeName (%q)",
			namespace,
			name,
			pvc.Status.Phase,
			pvc.Spec.VolumeName)
	}

	return pvc.Spec.VolumeName, pvc.UID, nil
}

// getPVSpecFromCache fetches the PV object with the given name from the shared
// internal PV store and returns a volume.Spec representing it.
// This method returns an error if a PV object does not exist in the cache with
// the given name.
// This method deep copies the PV object so the caller may use the returned
// volume.Spec object without worrying about it mutating unexpectedly.
func getPVSpecFromCache(name string, pvcReadOnly bool, expectedClaimUID types.UID, pvInformer kcache.SharedInformer) (*volume.Spec, error) {
	pvObj, exists, err := pvInformer.GetStore().GetByKey(name)
	if pvObj == nil || !exists || err != nil {
		return nil, fmt.Errorf(
			"failed to find PV %q in PVInformer cache. %v", name, err)
	}

	pv, ok := pvObj.(*api.PersistentVolume)
	if !ok || pv == nil {
		return nil, fmt.Errorf(
			"failed to cast %q object %#v to PersistentVolume", name, pvObj)
	}

	if pv.Spec.ClaimRef == nil {
		return nil, fmt.Errorf(
			"found PV object %q but it has a nil pv.Spec.ClaimRef indicating it is not yet bound to the claim",
			name)
	}

	if pv.Spec.ClaimRef.UID != expectedClaimUID {
		return nil, fmt.Errorf(
			"found PV object %q but its pv.Spec.ClaimRef.UID (%q) does not point to claim.UID (%q)",
			name,
			pv.Spec.ClaimRef.UID,
			expectedClaimUID)
	}

	// Do not return the object from the informer, since the store is shared it
	// may be mutated by another consumer.
	clonedPVObj, err := api.Scheme.DeepCopy(pv)
	if err != nil || clonedPVObj == nil {
		return nil, fmt.Errorf("failed to deep copy %q PV object. err=%v", name, err)
	}

	clonedPV, ok := clonedPVObj.(*api.PersistentVolume)
	if !ok {
		return nil, fmt.Errorf("failed to cast %q clonedPV %#v to PersistentVolume", name, pv)
	}

	return volume.NewSpecFromPersistentVolume(clonedPV, pvcReadOnly), nil
}

// ProcessPodVolumes processes the volumes in the given pod and adds them to the
// desired state of the world if addVolumes is true, otherwise it removes them.
func ProcessPodVolumes(pod *api.Pod, addVolumes bool, desiredStateOfWorld cache.DesiredStateOfWorld, volumePluginMgr *volume.VolumePluginMgr, pvcInformer kcache.SharedInformer, pvInformer kcache.SharedInformer) {
	if pod == nil {
		return
	}

	if len(pod.Spec.Volumes) <= 0 {
		glog.V(10).Infof("Skipping processing of pod %q/%q: it has no volumes.",
			pod.Namespace,
			pod.Name)
		return
	}

	nodeName := types.NodeName(pod.Spec.NodeName)
	if nodeName == "" {
		glog.V(10).Infof(
			"Skipping processing of pod %q/%q: it is not scheduled to a node.",
			pod.Namespace,
			pod.Name)
		return
	} else if !desiredStateOfWorld.NodeExists(nodeName) {
		// If the node the pod is scheduled to does not exist in the desired
		// state of the world data structure, that indicates the node is not
		// yet managed by the controller. Therefore, ignore the pod.
		glog.V(10).Infof(
			"Skipping processing of pod %q/%q: it is scheduled to node %q which is not managed by the controller.",
			pod.Namespace,
			pod.Name,
			nodeName)
		return
	}

	// Process volume spec for each volume defined in pod
	for _, podVolume := range pod.Spec.Volumes {
		volumeSpec, err := CreateVolumeSpec(podVolume, pod.Namespace, pvcInformer, pvInformer)
		if err != nil {
			glog.V(10).Infof(
				"Error processing volume %q for pod %q/%q: %v",
				podVolume.Name,
				pod.Namespace,
				pod.Name,
				err)
			continue
		}

		attachableVolumePlugin, err :=
			volumePluginMgr.FindAttachablePluginBySpec(volumeSpec)
		if err != nil || attachableVolumePlugin == nil {
			glog.V(10).Infof(
				"Skipping volume %q for pod %q/%q: it does not implement attacher interface. err=%v",
				podVolume.Name,
				pod.Namespace,
				pod.Name,
				err)
			continue
		}

		uniquePodName := volumehelper.GetUniquePodName(pod)
		if addVolumes {
			// Add volume to desired state of world
			_, err := desiredStateOfWorld.AddPod(
				uniquePodName, pod, volumeSpec, nodeName)
			if err != nil {
				glog.V(10).Infof(
					"Failed to add volume %q for pod %q/%q to desiredStateOfWorld. %v",
					podVolume.Name,
					pod.Namespace,
					pod.Name,
					err)
			}

		} else {
			// Remove volume from desired state of world
			uniqueVolumeName, err := volumehelper.GetUniqueVolumeNameFromSpec(
				attachableVolumePlugin, volumeSpec)
			if err != nil {
				glog.V(10).Infof(
					"Failed to delete volume %q for pod %q/%q from desiredStateOfWorld. GetUniqueVolumeNameFromSpec failed with %v",
					podVolume.Name,
					pod.Namespace,
					pod.Name,
					err)
				continue
			}
			desiredStateOfWorld.DeletePod(
				uniquePodName, uniqueVolumeName, nodeName)
		}
	}
	return
}
