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

package common

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"testing"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/utils"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/workflowproj"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
)

func Test_readImageBasicCredentials(t *testing.T) {
	basicAuthCredentials := remote.WithAuth(&authn.Basic{
		Username: "6340056|sonataflow-testing",
		Password: "eyJhbGciOiJSUzUxMiJ9.eyJzdWIiOiJjMzJhNjY1YjllNTc0NGJlYjllMDk0MzAxZDE5OTc5NiJ9.V-YMoFWLg2S7KC-6LEQZxcy9dp4-DppxSzPvFpMjO0YrhewBvoANszhweOmtIzjCUGbCFAqUpjYQDBWwma7brxP7KfaVrW9ayfQpo1Rr0eE5629k1OF4nIEWl4rxMZYf9UlO2Z-P7vjlD3AWaQ5EkF0AmKkEE6bwermh4VAj_932480HYalnTyNxtxFCNiU293G-V_XGtfpAeJlsALUJ-jWQhLvQbLfOQafUiftpqxA4PWfgWAid58yOHK0N_hPMFX0_R3rWTATlVsWy3U2QaRNzpQ-anxlYYa9escY30lkCNQidJdmfmt91oSDeI3ZdPojsqi6e2_SgnIzVwcD3IWlc8XAju4T5i605i6HYKP4z-NHcOWc9U_HUYd6MNERjv5DAFFSNnrRL2JASc1KeOfdxGPi87LWs8gXAzDscSeDDH9pwt1Iw1E9s3eNJLoanp0vdFOxzAxmKbwlUSc2wB244fic51QrSJ3KNqzmyEXUBpAKVezVt2hsJH-tmNsjvgz37N2U5o4m3zv56kpqnvqptBnUVRCqYTTF-Gvv_bRKTJw-Aww5dgWyTMkmkiVqJuw6nV-e5u40pogXAmdoXqJb4xS_2G7uKKS56Q15ppOU6QCQg6gKKHkYRymKkVLZpc_-sZnfAK3LXa7vxtrs8gltvvTmIrr4M70yf3KK06lQ",
	})
	image, err := utils.ReadImage("registry.redhat.io/rhel8/postgresql-15", basicAuthCredentials)
	assert.Nil(t, err)
	assert.NotNil(t, image)
}

func Test_readImageLocalCredentials(t *testing.T) {
	// use the values from current ~./docker/config.json -> remote.WithAuthFromKeychain(authn.DefaultKeychain))
	localCredentials := remote.WithAuthFromKeychain(authn.DefaultKeychain)
	image, err := utils.ReadImage("registry.redhat.io/rhel8/postgresql-15", localCredentials)
	assert.Nil(t, err)
	assert.NotNil(t, image)
}

/*
func Test_readImageSecrets(t *testing.T) {

	ctx := context.Background()

	// Load Kubernetes client config.
	// Try in-cluster first, fall back to local ~/.kube/config
	var cfg *rest.Config
	var err error
	cfg, err = rest.InClusterConfig()
	if err != nil {
		cfg, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			log.Fatalf("cannot load kube config: %v", err)
		}
	}

	k8sClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("creating k8s client: %v", err)
	}

	// Create a keychain from Kubernetes secrets in the given namespace.
	// It will automatically read Secrets of type kubernetes.io/dockerconfigjson.
	namespace := "default" // change to where your secret lives
	keychain, err := k8schain.New(ctx, k8sClient, k8schain.Options{
		Namespace:          namespace,
		ServiceAccountName: "default", // optional; include if SA has imagePullSecrets
	})
	if err != nil {
		log.Fatalf("creating keychain: %v", err)
	}

		// Now use the keychain to authenticate the remote image pull.
		ref, err := name.ParseReference("registry.redhat.io/rhel8/postgresql-15:latest")
		if err != nil {
			log.Fatalf("parsing reference: %v", err)
		}

		img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.NewMultiKeychain(keychain)))
		if err != nil {
			log.Fatalf("fetching image: %v", err)
		}

		digest, err := img.Digest()
		if err != nil {
			log.Fatalf("digest: %v", err)
		}

		fmt.Printf("Successfully pulled image %s\nDigest: %s\n", ref.Name(), digest.String())
	}


	image, err := ReadImage("registry.redhat.io/rhel8/postgresql-15", localCredentials)
	assert.Nil(t, err)
	assert.NotNil(t, image)
}
*/

func Test_readWorkflows(t *testing.T) {
	var err error
	//image, err := ReadImage("quay.io/wmedvede/serverless-workflow-operator-subflows:1.0-main-00")
	image, err := utils.ReadImage("registry.redhat.io/rhel8/postgresql-15")
	assert.Nil(t, err)

	pattern := regexp.MustCompile("serverless-workflow-project-[\\w.-]+\\.jar$")

	jar, err := utils.ReadFile(image, pattern)
	assert.Nil(t, err)
	files, err := workflowproj.ReadWorkflowFilesFromJar(jar.Content)
	assert.Nil(t, err)
	for fileName, content := range files {
		fmt.Printf("Workflow File: %s\n\n%s\n", fileName, string(content))
	}
}

// The main function that demonstrates reading and iterating through image layers.
func Test_readImage(t *testing.T) {
	// 1. Define the image reference
	// We'll use a small, publicly available image for this example.
	//imageRef := "cgr.dev/chainguard/static:latest"

	imageRef := "quay.io/wmedvede/serverless-workflow-operator-subflows:1.0-main-00"
	// 2. Parse the image reference
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		log.Fatalf("Error parsing reference %s: %v", imageRef, err)
	}

	// 3. Get the image from the remote registry
	img, err := remote.Image(ref)

	if err != nil {
		log.Fatalf("Error fetching remote image %s: %v", imageRef, err)
	}

	fmt.Printf("✅ Successfully fetched image: %s\n", imageRef)
	fmt.Println("---")

	hash, err := img.ConfigName()
	fmt.Printf("The hash: %s\n", hash.String())
	configFile, err := img.ConfigFile()
	fmt.Printf("RootFS: %s\n", configFile.RootFS)
	media, err := img.MediaType()
	fmt.Printf("media: %s\n", media)

	// 4. Iterate over the layers
	err = processLayers(img)
	if err != nil {
		log.Fatalf("Error processing layers: %v", err)
	}
}

// processLayers retrieves and iterates through the contents of each image layer.
func processLayers(img v1.Image) error {
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("could not get image layers: %w", err)
	}

	// Loop through each layer (ordered from base to top)
	for i, layer := range layers {
		digest, err := layer.Digest()
		if err != nil {
			return fmt.Errorf("could not get layer digest: %w", err)
		}

		fmt.Printf("Layer %d: %s\n", i+1, digest.String())

		// Get the compressed stream (gzipped tar) for the layer
		compressed, err := layer.Compressed()
		if err != nil {
			return fmt.Errorf("could not get compressed layer content: %w", err)
		}
		defer compressed.Close()

		// Get the uncompressed stream (raw tar)
		uncompressed, err := layer.Uncompressed()
		if err != nil {
			return fmt.Errorf("could not get uncompressed layer content: %w", err)
		}
		defer uncompressed.Close()

		// *** CORE LOGIC: Using archive/tar to iterate file entries ***

		// Create a tar.Reader to read the layer content
		tr := tar.NewReader(uncompressed)

		var fileCount int
		// Iterate through each file (header) in the tar archive
		for {
			header, err := tr.Next()

			if err == io.EOF {
				// End of tar archive (end of layer)
				break
			}
			if err != nil {
				return fmt.Errorf("error reading tar header: %w", err)
			}

			// Optional: Filter for a specific file/path you want to read
			if header.Name == "/etc/passwd" {
				fmt.Printf("  🚨 Found target file: %s (Size: %d bytes)\n", header.Name, header.Size)
				// To read the content of the file:
				// content, _ := io.ReadAll(tr)
				// fmt.Printf("Content: \n%s\n", content)
			}

			fmt.Printf("File name: %s\n", header.Name)

			if /*header.Typeflag == tar.TypeReg &&*/ strings.HasSuffix(header.Name, ".sw.yaml") || strings.HasSuffix(header.Name, ".sw.yml") || strings.HasSuffix(header.Name, ".sw.json") {
				fmt.Printf("Workflow file: %s\b", header.Name)

				buf := make([]byte, 1000000)
				n, _ := tr.Read(buf)
				fmt.Printf("  Content: %q\n", buf[:n])

			}

			if header.Name == "deployments/app/serverless-workflow-project-1.0.0-SNAPSHOT.jar" {

				fmt.Printf("Project found!\n")

				buffer := make([]byte, header.Size)

				if n, err := io.ReadFull(tr, buffer); err != nil {
					fmt.Printf("error reading %s, %v\n", header.Name, err)
				} else {
					fmt.Printf("buffer of size: %d, was read successfully, n: %d\n", header.Size, n)

					// Open the jar as a zip archive
					jarReader, err := zip.NewReader(bytes.NewReader(buffer), header.Size)
					if err != nil {
						panic(err)
					}

					// Iterate over files inside the jar
					for _, f := range jarReader.File {
						fmt.Println("Jar entry:", f.Name)
						if f.Name == "master.sw.yml" {
							// Open and read the file
							rc, err := f.Open()
							if err != nil {
								panic(err)
							}
							content, err := io.ReadAll(rc)
							rc.Close()
							if err != nil {
								panic(err)
							}

							fmt.Println("Found MyFile.json content:")
							fmt.Println(string(content))
						}
					}
				}
				/*
					if n, err := tr.Read(buffer); err != nil {
						fmt.Printf("error reading %s, %v\n", header.Name, err)
					} else {
						fmt.Printf("buffer of size: %d, was read successfully, n: %d\n", header.Size, n)
					}*/

			}
			// Skip directories
			if header.Typeflag == tar.TypeDir {
				continue
			}

			// Print general file information
			// fmt.Printf("  File: %s (Size: %d bytes, Type: %c)\n", header.Name, header.Size, header.Typeflag)
			fileCount++
		}
		fmt.Printf("  Total files found in layer: %d\n", fileCount)
		fmt.Println("---")
	}

	return nil
}
