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
	"context"
	"errors"
	"fmt"
	"os"

	k8sappsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var (
	// data volume names much have a "kubetap" prefix to be
	// properly removed during untapping.
	mitmproxyDataVolName = "kubetap-mitmproxy-data"
	mitmproxyConfigFile  = "config.yaml"
	mitmproxyBaseConfig  = `listen_port: 7777
ssl_insecure: true
web_port: 2244
web_host: 0.0.0.0
web_open_browser: false
`
)

// MitmproxySidecarContainer is the default proxy sidecar for HTTP Taps.
var MitmproxySidecarContainer = v1.Container{
	Name: kubetapContainerName,
	//Image:           image,       // Image is controlled by main
	//Args:            commandArgs, // Args is controlled by main
	ImagePullPolicy: v1.PullAlways,
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
	ReadinessProbe: &v1.Probe{
		Handler: v1.Handler{
			HTTPGet: &v1.HTTPGetAction{
				Path:   "/",
				Port:   intstr.FromInt(kubetapProxyWebInterfacePort),
				Scheme: v1.URISchemeHTTP,
			},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       5,
		SuccessThreshold:    3,
		TimeoutSeconds:      5,
	},
	VolumeMounts: []v1.VolumeMount{
		{
			//Name:    "", // Name is controlled by main
			MountPath: "/home/mitmproxy/config/",
			// We store outside main dir to prevent RO problems, see below.
			// This also means that we need to wrap the official mitmproxy container.
			/*
				// *sigh* https://github.com/kubernetes/kubernetes/issues/64120
				ReadOnly: false, // mitmproxy container does a chown
				MountPath: "/home/mitmproxy/.mitmproxy/config.yaml",
				SubPath:   "config.yaml", // we only mount the config file
			*/
		},
		{
			Name:      mitmproxyDataVolName,
			MountPath: "/home/mitmproxy/.mitmproxy",
			ReadOnly:  false,
		},
	},
}

// NewMitmproxy initializes a new mitmproxy Tap.
func NewMitmproxy(c kubernetes.Interface, p ProxyOptions) Tap {
	// mitmproxy only supports one mode right now.
	// How we expose options for other modes may
	// be explored in the future.
	p.Mode = "reverse"
	return &Mitmproxy{
		Protos:    []Protocol{protocolHTTP},
		Client:    c,
		ProxyOpts: p,
	}
}

// Mitmproxy is a interactive web proxy for intercepting and modifying HTTP requests.
type Mitmproxy struct {
	Protos    []Protocol
	Client    kubernetes.Interface
	ProxyOpts ProxyOptions
}

// Sidecar provides a proxy sidecar container.
func (m *Mitmproxy) Sidecar(deploymentName string) v1.Container {
	c := MitmproxySidecarContainer
	c.VolumeMounts[0].Name = kubetapConfigMapPrefix + deploymentName
	return c
}

// PatchDeployment provides any necessary tweaks to the deployment after the sidecar is added.
func (m *Mitmproxy) PatchDeployment(deployment *k8sappsv1.Deployment) {
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, v1.Volume{
		Name: kubetapConfigMapPrefix + deployment.Name,
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: kubetapConfigMapPrefix + deployment.Name,
				},
			},
		},
	})
	// add emptydir to resolve permission problems, and to down the road export dumps
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, v1.Volume{
		Name: mitmproxyDataVolName,
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{},
		},
	})
}

// Protocols returns a slice of protocols supported by Mitmproxy, currently only HTTP.
func (m *Mitmproxy) Protocols() []Protocol {
	return m.Protos
}

// String is called to conveniently print the type of Tap to stdout.
func (m *Mitmproxy) String() string {
	return "mitmproxy"
}

// ReadyEnv readies the environment by providing a ConfigMap for the mitmproxy container.
func (m *Mitmproxy) ReadyEnv() error {
	configmapsClient := m.Client.CoreV1().ConfigMaps(m.ProxyOpts.Namespace)
	// Create the ConfigMap based the options we're configuring mitmproxy with
	if err := createMitmproxyConfigMap(configmapsClient, m.ProxyOpts); err != nil {
		// If the service hasn't been tapped but still has a configmap from a previous
		// run (which can happen if the deployment borks and "tap off" isn't explicitly run,
		// delete the configmap and try again.
		// This is mostly here to fix development environments that become broken during
		// code testing.
		_ = destroyMitmproxyConfigMap(configmapsClient, m.ProxyOpts.dplName)
		rErr := createMitmproxyConfigMap(configmapsClient, m.ProxyOpts)
		if rErr != nil {
			if errors.Is(os.ErrInvalid, rErr) {
				return fmt.Errorf("there was an unexpected problem creating the ConfigMap")
			}
			return rErr
		}
	}
	return nil
}

// UnreadyEnv removes tap supporting configmap.
func (m *Mitmproxy) UnreadyEnv() error {
	configmapsClient := m.Client.CoreV1().ConfigMaps(m.ProxyOpts.Namespace)
	return destroyMitmproxyConfigMap(configmapsClient, m.ProxyOpts.dplName)
}

// createMitmproxyConfigMap creates a mitmproxy configmap based on the proxy mode, however currently
// only "reverse" mode is supported.
func createMitmproxyConfigMap(configmapClient corev1.ConfigMapInterface, proxyOpts ProxyOptions) error {
	// TODO: eventually, we should build a struct and use yaml to marshal this,
	// but for now we're just doing string concatenation.
	var mitmproxyConfig []byte
	switch proxyOpts.Mode {
	case "reverse":
		if proxyOpts.UpstreamHTTPS {
			mitmproxyConfig = append([]byte(mitmproxyBaseConfig), []byte("mode: reverse:https://127.0.0.1:"+proxyOpts.UpstreamPort)...)
		} else {
			mitmproxyConfig = append([]byte(mitmproxyBaseConfig), []byte("mode: reverse:http://127.0.0.1:"+proxyOpts.UpstreamPort)...)
		}
	case "regular":
		// non-applicable
		return errors.New("mitmproxy container only supports \"reverse\" mode")
	case "socks5":
		// non-applicable
		return errors.New("mitmproxy container only supports \"reverse\" mode")
	case "upstream":
		// non-applicable, unless you really know what you're doing, in which case fork this and connect it to your existing proxy
		return errors.New("mitmproxy container only supports \"reverse\" mode")
	case "transparent":
		// Because transparent mode uses iptables, it's not supported as we cannot guarantee that iptables is available and functioning
		return errors.New("mitmproxy container only supports \"reverse\" mode")
	default:
		return errors.New("invalid proxy mode: \"" + proxyOpts.Mode + "\"")
	}
	cmData := make(map[string][]byte)
	cmData[mitmproxyConfigFile] = mitmproxyConfig
	cm := v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubetapConfigMapPrefix + proxyOpts.dplName,
			Namespace: proxyOpts.Namespace,
			Annotations: map[string]string{
				annotationConfigMap: configMapAnnotationPrefix + proxyOpts.dplName,
			},
		},
		BinaryData: cmData,
	}
	slen := len(cm.BinaryData[mitmproxyConfigFile])
	if slen == 0 {
		return os.ErrInvalid
	}
	ccm, err := configmapClient.Create(context.TODO(), &cm, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	if ccm.BinaryData == nil {
		return os.ErrInvalid
	}
	cdata := ccm.BinaryData[mitmproxyConfigFile]
	if len(cdata) != slen {
		return ErrCreateResourceMismatch
	}
	return nil
}

// destroyMitmproxyConfigMap removes a mitmproxy ConfigMap from the environment.
func destroyMitmproxyConfigMap(configmapClient corev1.ConfigMapInterface, deploymentName string) error {
	if deploymentName == "" {
		return os.ErrInvalid
	}
	cms, err := configmapClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error getting ConfigMaps: %w", err)
	}
	var targetConfigMapNames []string
	for _, cm := range cms.Items {
		anns := cm.GetAnnotations()
		if anns == nil {
			continue
		}
		for k, v := range anns {
			if k == annotationConfigMap && v == configMapAnnotationPrefix+deploymentName {
				targetConfigMapNames = append(targetConfigMapNames, cm.Name)
			}
		}
	}
	if len(targetConfigMapNames) == 0 {
		return ErrConfigMapNoMatch
	}
	return configmapClient.Delete(context.TODO(), targetConfigMapNames[0], metav1.DeleteOptions{})
}
