/*
Copyright 2020 Noah Kantrowitz
Copyright 2019 Ridecell, Inc.

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

package randstring

import (
	"crypto/rand"
	"encoding/base64"
)

var RandEncoding = base64.NewEncoding("abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijkl").WithPadding(base64.NoPadding)

func RandomBytes(size int) ([]byte, error) {
	raw := make([]byte, size)
	_, err := rand.Read(raw)
	if err != nil {
		return nil, err
	}
	out := make([]byte, RandEncoding.EncodedLen(size))
	RandEncoding.Encode(out, raw)
	return out, nil
}

func RandomString(size int) (string, error) {
	randString, err := RandomBytes(size)
	return string(randString), err
}

func MustRandomBytes(size int) []byte {
	out, err := RandomBytes(size)
	if err != nil {
		panic(err)
	}
	return out
}

func MustRandomString(size int) string {
	out, err := RandomString(size)
	if err != nil {
		panic(err)
	}
	return out
}
