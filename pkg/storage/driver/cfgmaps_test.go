/*
Copyright The Helm Authors.
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

package driver

import (
	"encoding/base64"
	"reflect"
	"testing"
	"unsafe"

	"github.com/gogo/protobuf/proto"
	v1 "k8s.io/api/core/v1"

	rspb "k8s.io/helm/pkg/proto/hapi/release"
)

func TestConfigMapName(t *testing.T) {
	c := newTestFixtureCfgMaps(t)
	if c.Name() != ConfigMapsDriverName {
		t.Errorf("Expected name to be %q, got %q", ConfigMapsDriverName, c.Name())
	}
}

type visit struct {
	a1  unsafe.Pointer
	a2  unsafe.Pointer
	typ reflect.Type
}

func deepValueEqual(t *testing.T, v1, v2 reflect.Value, visited map[visit]bool, depth int) bool {
	if !v1.IsValid() || !v2.IsValid() {
		return v1.IsValid() == v2.IsValid()
	}
	if v1.Type() != v2.Type() {
		return false
	}

	// if depth > 10 { panic("deepValueEqual") }	// for debugging

	// We want to avoid putting more in the visited map than we need to.
	// For any possible reference cycle that might be encountered,
	// hard(t) needs to return true for at least one of the types in the cycle.
	hard := func(k reflect.Kind) bool {
		switch k {
		case reflect.Map, reflect.Slice, reflect.Ptr, reflect.Interface:
			return true
		}
		return false
	}

	if v1.CanAddr() && v2.CanAddr() && hard(v1.Kind()) {
		addr1 := unsafe.Pointer(v1.UnsafeAddr())
		addr2 := unsafe.Pointer(v2.UnsafeAddr())
		if uintptr(addr1) > uintptr(addr2) {
			// Canonicalize order to reduce number of entries in visited.
			// Assumes non-moving garbage collector.
			addr1, addr2 = addr2, addr1
		}

		// Short circuit if references are already seen.
		typ := v1.Type()
		v := visit{addr1, addr2, typ}
		if visited[v] {
			return true
		}

		// Remember for later.
		visited[v] = true
	}

	switch v1.Kind() {
	case reflect.Struct:
		t.Errorf("AAAA")
		for i, n := 0, v1.NumField(); i < n; i++ {
			if !deepValueEqual(t, v1.Field(i), v2.Field(i), visited, depth+1) {
				return false
			}
		}
		return true
	case reflect.Map:
		if v1.IsNil() != v2.IsNil() {
			return false
		}
		if v1.Len() != v2.Len() {
			return false
		}
		if v1.Pointer() == v2.Pointer() {
			return true
		}
		for _, k := range v1.MapKeys() {
			val1 := v1.MapIndex(k)
			val2 := v2.MapIndex(k)
			if !val1.IsValid() || !val2.IsValid() || !deepValueEqual(t, val1, val2, visited, depth+1) {
				return false
			}
		}
		return true
	default:
		// Normal equality suffices
		t.Errorf("|%+v|%+v|", v1, v2)
		t.Errorf("%v, %v", v1.Type(), v2.Type())
		t.Errorf("%+v", v1 == v2)
		return v1 == v2
		// return valueInterface(v1, false) == valueInterface(v2, false)
	}
}

func deepEqual(t *testing.T, x, y interface{}) bool {
	v1 := reflect.ValueOf(x)
	v2 := reflect.ValueOf(y)
	return deepValueEqual(t, v1, v2, make(map[visit]bool), 0)
}

func TestConfigMapGet(t *testing.T) {
	vers := int32(1)
	seed := int32(10)
	name := "smug-pigeon"
	namespace := "default"
	key := testKey(name, vers)
	rel := releaseStub(name, vers, seed, namespace, rspb.Status_DEPLOYED)

	cfgmaps := newTestFixtureCfgMaps(t, []*rspb.Release{rel}...)

	// get release with key
	got, err := cfgmaps.Get(key)
	if err != nil {
		t.Fatalf("Failed to get release: %s", err)
	}
	// compare fetched release with original
	if !reflect.DeepEqual(rel.Info, got.Info) {
		t.Errorf("Expected {%q}, got {%q}", rel.Info, got.Info)
	}
}

func TestUNcompressedConfigMapGet(t *testing.T) {
	vers := int32(1)
	seed := int32(10)
	name := "smug-pigeon"
	namespace := "default"
	key := testKey(name, vers)
	rel := releaseStub(name, vers, seed, namespace, rspb.Status_DEPLOYED)

	// Create a test fixture which contains an uncompressed release
	cfgmap, err := newConfigMapsObject(key, rel, nil)
	if err != nil {
		t.Fatalf("Failed to create configmap: %s", err)
	}
	b, err := proto.Marshal(rel)
	if err != nil {
		t.Fatalf("Failed to marshal release: %s", err)
	}
	cfgmap.Data["release"] = base64.StdEncoding.EncodeToString(b)
	var mock MockConfigMapsInterface
	mock.objects = map[string]*v1.ConfigMap{key: cfgmap}
	cfgmaps := NewConfigMaps(&mock)

	// get release with key
	got, err := cfgmaps.Get(key)
	if err != nil {
		t.Fatalf("Failed to get release: %s", err)
	}
	// compare fetched release with original
	if !reflect.DeepEqual(rel, got) {
		t.Errorf("Expected {%q}, got {%q}", rel, got)
	}
}

func TestConfigMapList(t *testing.T) {
	cfgmaps := newTestFixtureCfgMaps(t, []*rspb.Release{
		releaseStub("key-1", 1, 10, "default", rspb.Status_DELETED),
		releaseStub("key-2", 1, 10, "default", rspb.Status_DELETED),
		releaseStub("key-3", 1, 10, "default", rspb.Status_DEPLOYED),
		releaseStub("key-4", 1, 10, "default", rspb.Status_DEPLOYED),
		releaseStub("key-5", 1, 10, "default", rspb.Status_SUPERSEDED),
		releaseStub("key-6", 1, 10, "default", rspb.Status_SUPERSEDED),
	}...)

	// list all deleted releases
	del, err := cfgmaps.List(func(rel *rspb.Release) bool {
		return rel.Info.Status.Code == rspb.Status_DELETED
	})
	// check
	if err != nil {
		t.Errorf("Failed to list deleted: %s", err)
	}
	if len(del) != 2 {
		t.Errorf("Expected 2 deleted, got %d:\n%v\n", len(del), del)
	}

	// list all deployed releases
	dpl, err := cfgmaps.List(func(rel *rspb.Release) bool {
		return rel.Info.Status.Code == rspb.Status_DEPLOYED
	})
	// check
	if err != nil {
		t.Errorf("Failed to list deployed: %s", err)
	}
	if len(dpl) != 2 {
		t.Errorf("Expected 2 deployed, got %d", len(dpl))
	}

	// list all superseded releases
	ssd, err := cfgmaps.List(func(rel *rspb.Release) bool {
		return rel.Info.Status.Code == rspb.Status_SUPERSEDED
	})
	// check
	if err != nil {
		t.Errorf("Failed to list superseded: %s", err)
	}
	if len(ssd) != 2 {
		t.Errorf("Expected 2 superseded, got %d", len(ssd))
	}
}

func TestConfigMapCreate(t *testing.T) {
	cfgmaps := newTestFixtureCfgMaps(t)

	vers := int32(1)
	seed := int32(10)
	name := "smug-pigeon"
	namespace := "default"
	key := testKey(name, vers)
	rel := releaseStub(name, vers, seed, namespace, rspb.Status_DEPLOYED)

	// store the release in a configmap
	if err := cfgmaps.Create(key, rel); err != nil {
		t.Fatalf("Failed to create release with key %q: %s", key, err)
	}

	// get the release back
	got, err := cfgmaps.Get(key)
	if err != nil {
		t.Fatalf("Failed to get release with key %q: %s", key, err)
	}

	// compare created release with original
	if !reflect.DeepEqual(rel, got) {
		t.Errorf("Expected {%q}, got {%q}", rel, got)
	}
}

func TestConfigMapUpdate(t *testing.T) {
	vers := int32(1)
	seed := int32(10)
	name := "smug-pigeon"
	namespace := "default"
	key := testKey(name, vers)
	rel := releaseStub(name, vers, seed, namespace, rspb.Status_DEPLOYED)

	cfgmaps := newTestFixtureCfgMaps(t, []*rspb.Release{rel}...)

	// modify release status code
	rel.Info.Status.Code = rspb.Status_SUPERSEDED

	// perform the update
	if err := cfgmaps.Update(key, rel); err != nil {
		t.Fatalf("Failed to update release: %s", err)
	}

	// fetch the updated release
	got, err := cfgmaps.Get(key)
	if err != nil {
		t.Fatalf("Failed to get release with key %q: %s", key, err)
	}

	// check release has actually been updated by comparing modified fields
	if rel.Info.Status.Code != got.Info.Status.Code {
		t.Errorf("Expected status %s, got status %s", rel.Info.Status.Code, got.Info.Status.Code)
	}
}
