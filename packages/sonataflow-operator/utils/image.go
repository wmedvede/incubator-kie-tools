// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package utils

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"regexp"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	"k8s.io/client-go/kubernetes"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type File struct {
	Content []byte
	Name    string
}

// NewKeyChain creates an authentication keychain in the context of a kubernetes cluster.
// The returned keychain includes the imagePullSecrets declared in the serviceAccount, if any, plus the additional
// imagePullSecrets passed as parameters. All the k8s resources are resolved in the given namespace.
func NewKeyChain(ctx context.Context, cli kubernetes.Interface, namespace, serviceAccount string, imagePullSecrets []string) (authn.Keychain, error) {
	return k8schain.New(ctx, cli, k8schain.Options{
		Namespace:          namespace,
		ServiceAccountName: serviceAccount,
		ImagePullSecrets:   imagePullSecrets,
		UseMountSecrets:    false,
	})
}

// ReadImage reads an image from a given registry by passing a regular image reference, eg. quay.io/my-user/my-image:1.0.
// The options might include authentication parameters if needed, see NewKeyChain.
func ReadImage(imageRef string, options ...remote.Option) (v1.Image, error) {
	var ref name.Reference
	var err error
	var img v1.Image
	if ref, err = name.ParseReference(imageRef); err != nil {
		return nil, fmt.Errorf("failed to parse image reference: %s, %v", imageRef, err)
	}
	if img, err = remote.Image(ref, options...); err != nil {
		return nil, fmt.Errorf("failed to fetch image: %s, %v", imageRef, err)
	}
	return img, nil
}

// ReadFile given an image, iterates the layers looking for a tar entry with a name that matches the regex. The first
// occurrence is returned if any, nil if not found.
func ReadFile(img v1.Image, regex *regexp.Regexp) (*File, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("failed to get image layers: %v", err)
	}
	layerIdx := len(layers) - 1
	for layerIdx >= 0 {
		fmt.Printf("Processing layer: %d\n", layerIdx)
		uncompressed, err := layers[layerIdx].Uncompressed()
		if err != nil {
			return nil, fmt.Errorf("failed to get uncompressed reader for layer: %d, %v", layerIdx, err)
		}
		tarReader := tar.NewReader(uncompressed)
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				// end of tar archive (end of layer)
				break
			}
			if err != nil {
				uncompressed.Close()
				return nil, fmt.Errorf("failed to read next tar header for layer: %d, %v", layerIdx, err)
			}
			fmt.Printf("----> tarEntry: %s\n", header.Name)
			if regex.MatchString(header.Name) {
				buffer := make([]byte, header.Size)
				if _, err = io.ReadFull(tarReader, buffer); err != nil {
					uncompressed.Close()
					return nil, fmt.Errorf("failed to read file: %s from layer: %d, %v", header.Name, layerIdx, err)
				}
				return &File{
					Content: buffer,
					Name:    header.Name,
				}, nil
			}
		}
		uncompressed.Close()
		layerIdx--
	}
	return nil, nil
}
