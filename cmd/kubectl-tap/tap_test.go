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
	"bytes"
	"context"
	"errors"
	"io/ioutil"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	k8sappsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func Test_NewTapCommand(t *testing.T) {
	tests := []struct {
		Name       string
		ClientFunc func() *fake.Clientset
		ProxyPort  int32
		Namespace  string
		Err        error
	}{
		{"simple", fakeClientUntappedSimple, 80, "default", nil},
		{"multi_selector_match", fakeClientUntappedMultiSelectorMatch, 80, "default", nil},
		{"no_namespace", fakeClientUntappedSimple, 80, "", nil},
		{"stray_configmap", fakeClientUntappedWithConfigMap, 80, "default", nil},
		{"incorrect_namespace", fakeClientUntappedSimple, 80, "notexist", ErrNamespaceNotExist},
		{"incorrect_port", fakeClientUntappedSimple, 9999, "default", ErrServiceMissingPort},
		{"tapped_simple", fakeClientTappedSimple, 80, "default", ErrServiceTapped},
		{"missing_deployment", fakeClientUntappedWithoutDeployment, 80, "default", ErrServiceSelectorNoMatch},
		{"no_namespace_in_cluster", fakeClientUntappedNoNamespace, 80, "default", ErrNamespaceNotExist},
		{"deployment_without_labels", fakeClientUntappedNoLabels, 80, "default", ErrServiceSelectorNoMatch},
		{"service_without_selectors", fakeClientUntappedNoSelectors, 80, "default", ErrNoSelectors},
		{"multi_selector_partial_match", fakeClientUntappedMultiSelectorPartialMatch, 80, "default", ErrServiceSelectorNoMatch},
		{"multi_deployment_match", fakeClientUntappedMultiDeploymentMatch, 80, "default", ErrServiceSelectorMultiMatch},
		{"deployment_match_outside_namespace", fakeClientUntappedMatchOutsideNamespace, 80, "default", ErrServiceSelectorNoMatch},
	}
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			require := require.New(t)
			fakeClient := tc.ClientFunc()
			testViper := viper.New()
			testViper.Set("proxyPort", tc.ProxyPort)
			testViper.Set("namespace", tc.Namespace)
			cmd := &cobra.Command{}
			cmd.SetOutput(ioutil.Discard)
			err := NewTapCommand(fakeClient, &rest.Config{}, testViper)(cmd, []string{"sample-service"})
			if tc.Err != nil {
				require.NotNil(err)
				require.True(errors.Is(err, tc.Err))
			} else {
				require.Nil(err)
				fakeDeployment, err := fakeClient.AppsV1().Deployments("default").Get(context.TODO(), "sample-deployment", metav1.GetOptions{})
				require.Nil(err)
				require.Len(fakeDeployment.Spec.Template.Spec.Containers, 2, "sidecar was not successfully added to deployment spec")
			}
		})
	}
}

func Test_NewUntapCommand(t *testing.T) {
	tests := []struct {
		Name       string
		ClientFunc func() *fake.Clientset
		Namespace  string
		Service    string
		Err        error
	}{
		{"simple", fakeClientTappedSimple, "default", "sample-service", nil},
		{"untapped", fakeClientUntappedSimple, "default", "sample-service", nil},
		{"named_ports", fakeClientUntappedNamedPorts, "default", "sample-service", nil},
		{"incorrect_namespace", fakeClientUntappedSimple, "nsnotexist", "sample-service", nil},
		{"no_namespace_in_cluster", fakeClientUntappedNoNamespace, "none", "sample-service", ErrNamespaceNotExist},
		{"missing_deployment", fakeClientUntappedWithoutDeployment, "default", "sample-service", ErrServiceSelectorNoMatch},
		{"no_namespace_in_cluster", fakeClientUntappedNoNamespace, "default", "sample-service", ErrNamespaceNotExist},
		{"deployment_without_labels", fakeClientUntappedNoLabels, "default", "sample-service", ErrServiceSelectorNoMatch},
		{"service_without_selectors", fakeClientUntappedNoSelectors, "default", "sample-service", ErrNoSelectors},
		{"multi_selector_partial_match", fakeClientUntappedMultiSelectorPartialMatch, "default", "sample-service", ErrServiceSelectorNoMatch},
		{"multi_deployment_match", fakeClientUntappedMultiDeploymentMatch, "default", "sample-service", ErrServiceSelectorMultiMatch},
		{"deployment_match_outside_namespace", fakeClientUntappedMatchOutsideNamespace, "default", "sample-service", ErrServiceSelectorNoMatch},
	}
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			require := require.New(t)
			fakeClient := tc.ClientFunc()
			testViper := viper.New()
			b := bytes.NewBufferString("")
			cmd := &cobra.Command{}
			cmd.SetOutput(b)
			err := NewUntapCommand(fakeClient, testViper)(cmd, []string{tc.Service})
			if tc.Err != nil {
				require.NotNil(err)
				require.True(errors.Is(err, tc.Err))
			} else {
				require.Nil(err)
				out, err := ioutil.ReadAll(b)
				require.Nil(err)
				require.Contains(string(out), "Untapped Service \"", "did not untap successfully")
			}
		})
	}
}

func Test_NewListCommand(t *testing.T) {
	tests := []struct {
		Name        string
		ClientFunc  func() *fake.Clientset
		Namespace   string
		Err         error
		ExpectedOut string
	}{
		{"simple", fakeClientTappedSimple, "default", nil, "Tapped Services in the default namespace:\n\nsample-service\n"},
		{"all_namespaces", fakeClientTappedSimple, "", nil, "default/sample-service\n"},
		{"namespace_not_exist", fakeClientTappedSimple, "notexist", ErrNamespaceNotExist, ""},
		{"untapped", fakeClientUntappedSimple, "default", nil, "No Services in the default namespace are tapped.\n"},
		{"untapped_all_ns", fakeClientUntappedSimple, "", nil, "No Services are tapped.\n"},
	}
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			require := require.New(t)
			fakeClient := tc.ClientFunc()
			testViper := viper.New()
			testViper.Set("namespace", tc.Namespace)
			b := bytes.NewBufferString("")
			cmd := &cobra.Command{}
			cmd.SetOutput(b)
			err := NewListCommand(fakeClient, testViper)(cmd, []string{})
			if tc.Err != nil {
				require.NotNil(err)
				require.True(errors.Is(err, tc.Err))
			} else {
				require.Nil(err)
				out, err := ioutil.ReadAll(b)
				require.Nil(err)
				require.Contains(string(out), tc.ExpectedOut)
			}
		})
	}
}

func Test_DestroyConfigMap(t *testing.T) {
	tests := []struct {
		Name        string
		ServiceName string
		ClientFunc  func() *fake.Clientset
		Err         error
	}{
		{"simple", "sample-service", fakeClientTappedSimple, nil},
		{"untapped", "sample-service", fakeClientUntappedSimple, ErrConfigMapNoMatch},
		{"no_svc_name", "", fakeClientTappedSimple, os.ErrInvalid},
		{"missing_annotations", "sample-service", fakeClientTappedNilAnnotations, ErrConfigMapNoMatch},
	}
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			require := require.New(t)
			fakeClient := tc.ClientFunc()
			cmClient := fakeClient.CoreV1().ConfigMaps("default")
			err := destroyConfigMap(cmClient, tc.ServiceName)
			if tc.Err != nil {
				require.NotNil(err)
				require.True(errors.Is(err, tc.Err))
			} else {
				require.Nil(err)
			}
		})
	}
}

func fakeClientUntappedSimple() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sample-deployment",
				Namespace: "default",
				Annotations: map[string]string{
					"my-annotation": "some-annotation",
				},
				Labels: map[string]string{
					"app": "myapp",
				},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
							},
						},
					},
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sample-service",
				Namespace: "default",
				Annotations: map[string]string{
					"my-annotation": "some-annotation",
				},
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
				},
				Selector: map[string]string{
					"app": "myapp",
				},
			},
		},
	)
}

func fakeClientTappedSimple() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-deployment",
				Namespace:   "default",
				Annotations: map[string]string{},
				Labels: map[string]string{
					"app": "myapp",
				},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
							},
							{
								Name:  kubetapContainerName,
								Image: "gcr.io/soluble-oss/kubetap-mitmproxy:latest",
							},
						},
						Volumes: []v1.Volume{
							{
								Name: kubetapConfigMapPrefix + "sample-deployment",
							},
						},
					},
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sample-service",
				Namespace: "default",
				Annotations: map[string]string{
					annotationOriginalTargetPort: "8080",
				},
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
					{
						Name:       kubetapServicePortName,
						Port:       kubetapProxyWebInterfacePort,
						TargetPort: intstr.FromInt(kubetapProxyListenPort),
					},
				},
				Selector: map[string]string{
					"app": "myapp",
				},
			},
		},
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kubetapConfigMapPrefix + "sample-deployment",
				Namespace: "default",
				Annotations: map[string]string{
					annotationConfigMap: configMapAnnotationPrefix + "sample-service",
				},
			},
		},
	)
}

func fakeClientUntappedWithConfigMap() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-deployment",
				Namespace:   "default",
				Annotations: map[string]string{},
				Labels: map[string]string{
					"app": "myapp",
				},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
							},
						},
					},
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-service",
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
				},
				Selector: map[string]string{
					"app": "myapp",
				},
			},
		},
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kubetapConfigMapPrefix + "sample-deployment",
				Namespace: "default",
				Annotations: map[string]string{
					annotationConfigMap: configMapAnnotationPrefix + "sample-service",
				},
			},
		},
	)
}

func fakeClientUntappedWithoutDeployment() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-service",
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
				},
				Selector: map[string]string{
					"app": "myapp",
				},
			},
		},
	)
}

func fakeClientUntappedNoNamespace() *fake.Clientset {
	return fake.NewSimpleClientset(
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-deployment",
				Namespace:   "default",
				Annotations: map[string]string{},
				Labels: map[string]string{
					"app": "myapp",
				},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
							},
						},
					},
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-service",
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
				},
				Selector: map[string]string{
					"app": "myapp",
				},
			},
		},
	)
}

func fakeClientUntappedNoLabels() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-deployment",
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
							},
						},
					},
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-service",
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
				},
				Selector: map[string]string{
					"app": "myapp",
				},
			},
		},
	)
}

func fakeClientUntappedNoSelectors() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-deployment",
				Namespace:   "default",
				Annotations: map[string]string{},
				Labels: map[string]string{
					"app": "myapp",
				},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
							},
						},
					},
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-service",
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
				},
			},
		},
	)
}

func fakeClientUntappedMultiSelectorMatch() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-deployment",
				Namespace:   "default",
				Annotations: map[string]string{},
				Labels: map[string]string{
					"app": "myapp",
					"foo": "bar",
				},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
							},
						},
					},
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-service",
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
				},
				Selector: map[string]string{
					"app": "myapp",
					"foo": "bar",
				},
			},
		},
	)
}

func fakeClientUntappedMultiSelectorPartialMatch() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-deployment",
				Namespace:   "default",
				Annotations: map[string]string{},
				Labels: map[string]string{
					"app": "myapp",
				},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
							},
						},
					},
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-service",
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
				},
				Selector: map[string]string{
					"app": "myapp",
					"foo": "bar",
				},
			},
		},
	)
}

func fakeClientUntappedMultiDeploymentMatch() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-deployment",
				Namespace:   "default",
				Annotations: map[string]string{},
				Labels: map[string]string{
					"app": "myapp",
				},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
							},
						},
					},
				},
			},
		},
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "different-sample-deployment",
				Namespace:   "default",
				Annotations: map[string]string{},
				Labels: map[string]string{
					"app": "myapp",
				},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
							},
						},
					},
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-service",
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
				},
				Selector: map[string]string{
					"app": "myapp",
				},
			},
		},
	)
}

func fakeClientUntappedMatchOutsideNamespace() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-deployment",
				Namespace:   "different-and-not-default",
				Annotations: map[string]string{},
				Labels: map[string]string{
					"app": "myapp",
				},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
							},
						},
					},
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "sample-service",
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
				},
				Selector: map[string]string{
					"app": "myapp",
				},
			},
		},
	)
}

func fakeClientTappedNilAnnotations() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sample-deployment",
				Namespace: "default",
				Labels: map[string]string{
					"app": "myapp",
				},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
							},
							{
								Name:  kubetapContainerName,
								Image: "gcr.io/soluble-oss/kubetap-mitmproxy:latest",
							},
						},
						Volumes: []v1.Volume{
							{
								Name: kubetapConfigMapPrefix + "sample-deployment",
							},
						},
					},
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sample-service",
				Namespace: "default",
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
					{
						Name:       kubetapServicePortName,
						Port:       kubetapProxyWebInterfacePort,
						TargetPort: intstr.FromInt(kubetapProxyListenPort),
					},
				},
				Selector: map[string]string{
					"app": "myapp",
				},
			},
		},
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kubetapConfigMapPrefix + "sample-deployment",
				Namespace: "default",
			},
		},
	)
}

func fakeClientUntappedNamedPorts() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		&k8sappsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sample-deployment",
				Namespace: "default",
				Annotations: map[string]string{
					"my-annotation": "some-annotation",
				},
				Labels: map[string]string{
					"app": "myapp",
				},
			},
			Spec: k8sappsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "someapp",
								Image: "gcr.io/soluble-oss/someapp:latest",
								Ports: []v1.ContainerPort{
									{
										Name:          kubetapPortName,
										ContainerPort: kubetapProxyListenPort,
										Protocol:      v1.ProtocolTCP,
									},
									{
										Name:          kubetapWebPortName,
										ContainerPort: kubetapProxyWebInterfacePort,
										Protocol:      v1.ProtocolTCP,
									},
								},
							},
						},
					},
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sample-service",
				Namespace: "default",
				Annotations: map[string]string{
					"my-annotation": "some-annotation",
				},
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "servicePortOne",
						Port:       80,
						TargetPort: intstr.FromString(kubetapPortName),
					},
				},
				Selector: map[string]string{
					"app": "myapp",
				},
			},
		},
	)
}
