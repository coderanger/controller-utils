/*
Copyright 2020 Noah Kantrowitz
Copyright 2018-2019 Ridecell, Inc.

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

package templates

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"path"
	"reflect"
	"text/template"

	"github.com/Masterminds/sprig"
	"github.com/shurcooL/httpfs/path/vfspath"
	"github.com/shurcooL/httpfs/vfsutil"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/coderanger/controller-utils/core"
)

func parseTemplate(fs http.FileSystem, filename string) (*template.Template, error) {
	if fs == nil {
		return nil, errors.New("template filesystem not set")
	}

	// Wrote this because if statements with pointers don't work how you'd think they would
	customFuncMap := template.FuncMap{
		"deref": func(input interface{}) interface{} {
			val := reflect.ValueOf(input)
			if val.IsNil() {
				return nil
			}
			return val.Elem().Interface()
		},
	}

	// Create a template object.
	tmpl := template.New(path.Base(filename)).Funcs(sprig.TxtFuncMap()).Funcs(customFuncMap)

	// Parse any helpers if present.
	helpers, err := vfspath.Glob(fs, "helpers/*.tpl")
	if err != nil {
		return nil, err
	}
	for _, helperFilename := range helpers {
		fileBytes, err := vfsutil.ReadFile(fs, helperFilename)
		if err != nil {
			return nil, err
		}

		_, err = tmpl.Parse(string(fileBytes))
		if err != nil {
			return nil, err
		}
	}

	// Parse the main template.
	fileBytes, err := vfsutil.ReadFile(fs, filename)
	if err != nil {
		return nil, err
	}

	_, err = tmpl.Parse(string(fileBytes))
	if err != nil {
		return nil, err
	}

	return tmpl, nil
}

func renderTemplate(tmpl *template.Template, data interface{}) ([]byte, error) {
	var buffer bytes.Buffer
	err := tmpl.Execute(&buffer, data)
	if err != nil {
		return []byte{}, err
	}

	return buffer.Bytes(), nil
}

// Parse the rendered data into an object. The caller has to cast it from a
// core.Object into the correct type.
func parseObject(rawObject []byte) (core.Object, error) {
	obj, _, err := scheme.Codecs.UniversalDeserializer().Decode(rawObject, nil, nil)
	if err != nil {
		return nil, err
	}
	coreObj, ok := obj.(core.Object)
	if !ok {
		return nil, errors.New("unable to cast to core.Object")
	}
	return coreObj, nil
}

func castArray(in []interface{}) []interface{} {
	result := make([]interface{}, len(in))
	for i, v := range in {
		result[i] = castValue(v)
	}
	return result
}

func castMap(in map[interface{}]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range in {
		kString, ok := k.(string)
		if !ok {
			kString = fmt.Sprintf("%v", k)
		}
		result[kString] = castValue(v)
	}
	return result
}

func castValue(v interface{}) interface{} {
	switch v := v.(type) {
	case []interface{}:
		return castArray(v)
	case map[interface{}]interface{}:
		return castMap(v)
	default:
		return v
	}
}

// Parse the rendered data into an Unstructured.
func parseUnstructured(rawObject []byte) (core.Object, error) {
	data := map[interface{}]interface{}{}
	err := yaml.Unmarshal(rawObject, data)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: castMap(data)}, nil
}

func Get(fs http.FileSystem, filename string, unstructured bool, data interface{}) (core.Object, error) {
	tmpl, err := parseTemplate(fs, filename)
	if err != nil {
		return nil, err
	}
	out, err := renderTemplate(tmpl, data)
	if err != nil {
		return nil, err
	}
	var obj core.Object
	if unstructured {
		obj, err = parseUnstructured(out)
	} else {
		obj, err = parseObject(out)
	}
	if err != nil {
		return nil, err
	}
	return obj, nil
}
