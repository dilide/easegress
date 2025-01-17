/*
 * Copyright (c) 2017, MegaEase
 * All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package api

import (
	"fmt"
	"io/ioutil"
	"sort"

	"github.com/megaease/easegress/pkg/supervisor"

	"github.com/kataras/iris"
	yaml "gopkg.in/yaml.v2"
)

const (
	// ObjectPrefix is the object prefix.
	ObjectPrefix = "/objects"

	// ObjectKindsPrefix is the object-kinds prefix.
	ObjectKindsPrefix = "/object-kinds"

	// StatusObjectPrefix is the prefix of object status.
	StatusObjectPrefix = "/status/objects"
)

func (s *Server) setupObjectAPIs() {
	objAPIs := make([]*APIEntry, 0)
	objAPIs = append(objAPIs,
		&APIEntry{
			Path:    ObjectKindsPrefix,
			Method:  "GET",
			Handler: s.listObjectKinds,
		},

		&APIEntry{
			Path:    ObjectPrefix,
			Method:  "POST",
			Handler: s.createObject,
		},
		&APIEntry{
			Path:    ObjectPrefix,
			Method:  "GET",
			Handler: s.listObjects,
		},

		&APIEntry{
			Path:    ObjectPrefix + "/{name:string}",
			Method:  "GET",
			Handler: s.getObject,
		},
		&APIEntry{
			Path:    ObjectPrefix + "/{name:string}",
			Method:  "PUT",
			Handler: s.updateObject,
		},
		&APIEntry{
			Path:    ObjectPrefix + "/{name:string}",
			Method:  "DELETE",
			Handler: s.deleteObject,
		},

		&APIEntry{
			Path:    StatusObjectPrefix,
			Method:  "GET",
			Handler: s.listStatusObjects,
		},
		&APIEntry{
			Path:    StatusObjectPrefix + "/{name:string}",
			Method:  "GET",
			Handler: s.getStatusObject,
		},
	)

	s.RegisterAPIs(objAPIs)
}

func (s *Server) readObjectSpec(ctx iris.Context) (*supervisor.Spec, error) {
	body, err := ioutil.ReadAll(ctx.Request().Body)
	if err != nil {
		return nil, fmt.Errorf("read body failed: %v", err)
	}

	spec, err := supervisor.NewSpec(string(body))
	if err != nil {
		return nil, err
	}

	name := ctx.Params().Get("name")

	if name != "" && name != spec.Name() {
		return nil, fmt.Errorf("inconsistent name in url and spec ")
	}

	return spec, err
}

func (s *Server) upgradeConfigVersion(ctx iris.Context) {
	version := s._plusOneVersion()
	ctx.ResponseWriter().Header().Set(ConfigVersionKey, fmt.Sprintf("%d", version))
}

func (s *Server) createObject(ctx iris.Context) {
	spec, err := s.readObjectSpec(ctx)
	if err != nil {
		HandleAPIError(ctx, iris.StatusBadRequest, err)
		return
	}

	name := spec.Name()

	s.Lock()
	defer s.Unlock()

	existedSpec := s._getObject(name)
	if existedSpec != nil {
		HandleAPIError(ctx, iris.StatusConflict, fmt.Errorf("conflict name: %s", name))
		return
	}

	s._putObject(spec)
	s.upgradeConfigVersion(ctx)

	ctx.StatusCode(iris.StatusCreated)
	location := fmt.Sprintf("%s/%s", ctx.Path(), name)
	ctx.Header("Location", location)
}

func (s *Server) deleteObject(ctx iris.Context) {
	name := ctx.Params().Get("name")

	s.Lock()
	defer s.Unlock()

	spec := s._getObject(name)
	if spec == nil {
		HandleAPIError(ctx, iris.StatusNotFound, fmt.Errorf("not found"))
		return
	}

	s._deleteObject(name)
	s.upgradeConfigVersion(ctx)
}

func (s *Server) getObject(ctx iris.Context) {
	name := ctx.Params().Get("name")

	// No need to lock.

	spec := s._getObject(name)
	if spec == nil {
		HandleAPIError(ctx, iris.StatusNotFound, fmt.Errorf("not found"))
		return
	}

	// Reference: https://mailarchive.ietf.org/arch/msg/media-types/e9ZNC0hDXKXeFlAVRWxLCCaG9GI
	ctx.Header("Content-Type", "text/vnd.yaml")
	ctx.Write([]byte(spec.YAMLConfig()))
}

func (s *Server) updateObject(ctx iris.Context) {
	spec, err := s.readObjectSpec(ctx)
	if err != nil {
		HandleAPIError(ctx, iris.StatusBadRequest, err)
		return
	}

	name := spec.Name()

	s.Lock()
	defer s.Unlock()

	existedSpec := s._getObject(name)
	if existedSpec == nil {
		HandleAPIError(ctx, iris.StatusNotFound, fmt.Errorf("not found"))
		return
	}

	if existedSpec.Kind() != spec.Kind() {
		HandleAPIError(ctx, iris.StatusBadRequest,
			fmt.Errorf("different kinds: %s, %s",
				existedSpec.Kind(), spec.Kind()))
		return
	}

	s._putObject(spec)
	s.upgradeConfigVersion(ctx)
}

func (s *Server) listObjects(ctx iris.Context) {
	// No need to lock.

	specs := specList(s._listObjects())
	// NOTE: Keep it consistent.
	sort.Sort(specs)

	buff, err := specs.Marshal()
	if err != nil {
		panic(err)
	}

	ctx.Header("Content-Type", "text/vnd.yaml")
	ctx.Write(buff)
}

func (s *Server) getStatusObject(ctx iris.Context) {
	name := ctx.Params().Get("name")

	spec := s._getObject(name)

	if spec == nil {
		HandleAPIError(ctx, iris.StatusNotFound, fmt.Errorf("not found"))
		return
	}

	// NOTE: Maybe inconsistent, the object was deleted already here.
	status := s._getStatusObject(name)

	buff, err := yaml.Marshal(status)
	if err != nil {
		panic(fmt.Errorf("marshal %#v to yaml failed: %v", status, err))
	}

	ctx.Header("Content-Type", "text/vnd.yaml")
	ctx.Write(buff)
}

func (s *Server) listStatusObjects(ctx iris.Context) {
	// No need to lock.

	status := s._listStatusObjects()

	buff, err := yaml.Marshal(status)
	if err != nil {
		panic(fmt.Errorf("marshal %#v to yaml failed: %v", status, err))
	}

	ctx.Header("Content-Type", "text/vnd.yaml")
	ctx.Write(buff)
}

type specList []*supervisor.Spec

func (s specList) Less(i, j int) bool { return s[i].Name() < s[j].Name() }
func (s specList) Len() int           { return len(s) }
func (s specList) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s specList) Marshal() ([]byte, error) {
	specs := []map[string]interface{}{}
	for _, spec := range s {
		var m map[string]interface{}
		err := yaml.Unmarshal([]byte(spec.YAMLConfig()), &m)
		if err != nil {
			return nil, fmt.Errorf("unmarshal %s to yaml failed: %v",
				spec.YAMLConfig(), err)
		}
		specs = append(specs, m)
	}

	buff, err := yaml.Marshal(specs)
	if err != nil {
		return nil, fmt.Errorf("marshal %#v to yaml failed: %v", specs, err)
	}

	return buff, nil
}

func (s *Server) listObjectKinds(ctx iris.Context) {
	kinds := supervisor.ObjectKinds()
	buff, err := yaml.Marshal(kinds)
	if err != nil {
		panic(fmt.Errorf("marshal %#v to yaml failed: %v", kinds, err))
	}

	ctx.Write(buff)
}
