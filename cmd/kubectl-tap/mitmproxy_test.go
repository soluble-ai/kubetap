// Copyright 2020 Soluble Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func Test_DestroyMitmproxyConfigMap(t *testing.T) {
	tests := []struct {
		Name           string
		DeploymentName string
		ClientFunc     func() *fake.Clientset
		Err            error
	}{
		{"simple", "sample-deployment", fakeClientTappedSimple, nil},
		{"untapped", "sample-deployment", fakeClientUntappedSimple, ErrConfigMapNoMatch},
		{"no_svc_name", "", fakeClientTappedSimple, os.ErrInvalid},
		{"missing_annotations", "sample-deployment", fakeClientTappedWithoutAnnotations, ErrConfigMapNoMatch},
	}
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			require := require.New(t)
			fakeClient := tc.ClientFunc()
			cmClient := fakeClient.CoreV1().ConfigMaps("default")
			err := destroyMitmproxyConfigMap(cmClient, tc.DeploymentName)
			if tc.Err != nil {
				require.NotNil(err)
				require.True(errors.Is(err, tc.Err))
			} else {
				require.Nil(err)
			}
		})
	}
}
