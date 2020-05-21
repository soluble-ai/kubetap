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
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	k8sappsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/client-go/util/retry"

	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	kubetapContainerName         = "kubetap"
	kubetapServicePortName       = "kubetap-web"
	kubetapPortName              = "kubetap-listen" // must be < 15 bytes
	kubetapWebPortName           = "kubetap-web"
	kubetapProxyListenPort       = 7777
	kubetapProxyWebInterfacePort = 2244
	kubetapConfigMapPrefix       = "kubetap-target-"

	mitmproxyDataVolName = "mitmproxy-data"
	mitmproxyConfigFile  = "config.yaml"
	mitmproxyBaseConfig  = `listen_port: 7777
ssl_insecure: true
web_port: 2244
web_host: 0.0.0.0
web_open_browser: false
`

	interactiveTimeoutSeconds = 90
	configMapAnnotationPrefix = "target-"
)

var (
	ErrNamespaceNotExist          = errors.New("the provided Namespace does not exist")
	ErrServiceMissingPort         = errors.New("the target Service does not have the provided port")
	ErrServiceTapped              = errors.New("the target Service has already been tapped")
	ErrServiceSelectorNoMatch     = errors.New("the Service selector did not match any Deployments")
	ErrServiceSelectorMultiMatch  = errors.New("the Service selector matched multiple Deployments")
	ErrDeploymentOutsideNamespace = errors.New("the Service selector matched Deployment outside the specified Namespace")
	ErrNoSelectors                = errors.New("no selectors are set for the target Service")
	ErrConfigMapNoMatch           = errors.New("the ConfigMap list did not match any ConfigMaps")
	ErrKubetapPodNoMatch          = errors.New("a Kubetap Pod was not found")
	ErrCreateResourceMismatch     = errors.New("the created resource did not match the desired state")
)

// ProxyOptions are options used to configure the mitmproxy configmap
// We will eventually provide explicit support for modes, and methods
// which validate the configuration for a given mode will likely exist
// in the future.
type ProxyOptions struct {
	Target        string
	UpstreamHTTPS bool
	UpstreamPort  string
	Mode          string
	Namespace     string
	Image         string
}

// NewListCommand lists Services that are already tapped.
func NewListCommand(client kubernetes.Interface, viper *viper.Viper) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		namespace := viper.GetString("namespace")
		exists, err := hasNamespace(client, namespace)
		if err != nil {
			// this is the one case where we allow an empty namespace string
			if !errors.Is(err, os.ErrInvalid) {
				return fmt.Errorf("error fetching namespaces: %w", err)
			}
			exists = true
		}
		if !exists {
			return ErrNamespaceNotExist
		}
		services, err := client.CoreV1().Services(namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		tappedServices := make(map[string]string)
		for _, svc := range services.Items {
			if svc.Annotations[annotationOriginalTargetPort] != "" {
				tappedServices[svc.Name] = svc.Namespace
			}
		}

		if namespace != "" {
			if len(tappedServices) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No Services in the %s namespace are tapped.\n", namespace)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Tapped Services in the %s namespace:\n", namespace)
			fmt.Fprintln(cmd.OutOrStdout())
			for k := range tappedServices {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", k)
			}
			return nil
		}
		if len(tappedServices) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No Services are tapped.")
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Tapped Namespace/Service:")
		for k, v := range tappedServices {
			fmt.Fprintf(cmd.OutOrStdout(), "%s/%s\n", v, k)
		}
		return nil
	}
}

// hasNamespace checks if a given Namespace exists.
func hasNamespace(client kubernetes.Interface, namespace string) (bool, error) {
	if namespace == "" {
		return false, os.ErrInvalid
	}
	ns, err := client.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	for _, n := range ns.Items {
		if n.Name == namespace {
			return true, nil
		}
	}
	return false, nil
}

// NewTapCommand identifies a target employment through service selectors and modifies that
// deployment to add a mitmproxy sidecar and configmap, then adjusts the service targetPort
// to point to the mitmproxy sidecar. Mitmproxy's ConfigMap sets the upstream to the original
// service destination.
func NewTapCommand(client kubernetes.Interface, config *rest.Config, viper *viper.Viper) func(*cobra.Command, []string) error { //nolint: gocyclo
	return func(cmd *cobra.Command, args []string) error {
		targetSvcName := args[0]

		targetSvcPort := viper.GetInt32("proxyPort")
		namespace := viper.GetString("namespace")
		image := viper.GetString("proxyImage")
		https := viper.GetBool("https")
		portForward := viper.GetBool("portForward")
		openBrowser := viper.GetBool("browser")
		if openBrowser {
			portForward = true
		}
		commandArgs := strings.Fields(viper.GetString("commandArgs"))
		if targetSvcPort == 0 {
			return fmt.Errorf("--port flag not provided")
		}
		if namespace == "" {
			namespace = "default"
		}
		exists, err := hasNamespace(client, namespace)
		if err != nil {
			return fmt.Errorf("error fetching namespaces: %w", err)
		}
		if !exists {
			return ErrNamespaceNotExist
		}

		proxyOpts := ProxyOptions{
			Target:        targetSvcName,
			UpstreamHTTPS: https,
			Mode:          "reverse", // eventually this may be configurable
			Namespace:     namespace,
		}

		deploymentsClient := client.AppsV1().Deployments(namespace)
		servicesClient := client.CoreV1().Services(namespace)
		configmapsClient := client.CoreV1().ConfigMaps(namespace)

		// get the service to ensure it exists before we go around monkeying with deployments
		targetService, err := client.CoreV1().Services(namespace).Get(context.TODO(), targetSvcName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// ensure that we haven't tapped this service already
		anns := targetService.GetAnnotations()
		if anns[annotationOriginalTargetPort] != "" {
			return ErrServiceTapped
		}

		// set the upstream port so the proxy knows where to forward traffic
		for _, ports := range targetService.Spec.Ports {
			if ports.Port != targetSvcPort {
				continue
			}
			if ports.TargetPort.Type == intstr.Int {
				proxyOpts.UpstreamPort = ports.TargetPort.String()
			}
			// if named, must determine port from deployment spec
			if ports.TargetPort.Type == intstr.String {
				dp, err := deploymentFromSelectors(deploymentsClient, targetService.Spec.Selector)
				if err != nil {
					return fmt.Errorf("error resolving TargetPort in Deployment: %w", err)
				}
				for _, c := range dp.Spec.Template.Spec.Containers {
					for _, p := range c.Ports {
						if p.Name == ports.TargetPort.String() {
							proxyOpts.UpstreamPort = strconv.Itoa(int(p.ContainerPort))
						}
					}
				}
			}
		}

		// Create the ConfigMap based the options we're configuring mitmproxy with
		if err := createConfigMap(configmapsClient, proxyOpts); err != nil {
			// If the service hasn't been tapped but still has a configmap from a previous
			// run (which can happen if the deployment borks and "tap off" isn't explicitly run,
			// delete the configmap and try again.
			// This is mostly here to fix development environments that become broken during
			// code testing.
			_ = destroyConfigMap(configmapsClient, proxyOpts.Target)
			rErr := createConfigMap(configmapsClient, proxyOpts)
			if rErr != nil {
				if errors.Is(os.ErrInvalid, rErr) {
					return fmt.Errorf("there was an unexpected problem creating the ConfigMap")
				}
				return rErr
			}
		}

		// Modify the Deployment to inject mitmproxy sidecar
		if err := createSidecars(deploymentsClient, targetService.Spec.Selector, image, commandArgs); err != nil {
			fmt.Fprintln(cmd.OutOrStdout(), "Error modifying Deployment, reverting tap...")
			_ = NewUntapCommand(client, viper)(cmd, args)
			return err
		}
		// Tap the service to redirect the incoming traffic to our proxy, which is configured to redirect
		// to the original port.
		if err := tapSvc(servicesClient, targetSvcName, targetSvcPort); err != nil {
			fmt.Fprintln(cmd.OutOrStdout(), "Error modifying Service, reverting tap...")
			_ = NewUntapCommand(client, viper)(cmd, args)
			return err
		}

		if !portForward {
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintf(cmd.OutOrStdout(), "Port %d of Service %q has been tapped!\n\n", targetSvcPort, targetSvcName)
			fmt.Fprintf(cmd.OutOrStdout(), "You can access the MITMproxy web interface at http://127.0.0.1:2244\n")
			fmt.Fprintf(cmd.OutOrStdout(), "after running the following command:\n\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  kubectl port-forward svc/%s -n %s 2244:2244\n\n", targetSvcName, namespace)
			fmt.Fprintf(cmd.OutOrStdout(), "If the Service is not publicly exposed through an Ingress,\n")
			fmt.Fprintf(cmd.OutOrStdout(), "you can access it with the following command:\n\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  kubectl port-forward svc/%s -n %s 4000:%d\n\n", targetSvcName, namespace, targetSvcPort)
			fmt.Fprintf(cmd.OutOrStdout(), "In the future, you can run with --port-forward or --browser to automate this process.\n")
			return nil
		}

		// We're now in an interactive state
		fmt.Fprintf(cmd.OutOrStdout(), "Establishing port-forward tunnels to Service...\n")
		berr := bytes.NewBufferString("")
		bout := bytes.NewBufferString("")
		stopCh := make(chan struct{})
		readyCh := make(chan struct{})
		ic := make(chan os.Signal, 1)
		signal.Notify(ic, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
		go func() {
			<-ic
			close(stopCh)
			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintln(cmd.OutOrStdout(), "Stopping kubetap...")
			_ = NewUntapCommand(client, viper)(cmd, args)
			die()
		}()

		podsClient := client.CoreV1().Pods(namespace)
		fmt.Fprintf(cmd.OutOrStdout(), "Waiting for Pod containers to become ready...")
		for i := 0; i < interactiveTimeoutSeconds; i++ {
			time.Sleep(1 * time.Second)
			fmt.Fprintf(cmd.OutOrStdout(), ".")
			// skip the first few checks to give pods time to come up
			if i < 5 {
				continue
			}
			dp, err := deploymentFromSelectors(deploymentsClient, targetService.Spec.Selector)
			if err != nil {
				return err
			}
			pod, err := kubetapPod(podsClient, dp.Name)
			if err != nil {
				return err
			}
			var ready bool
			for _, cond := range pod.Status.Conditions {
				if cond.Type == "ContainersReady" {
					if cond.Status == "True" {
						ready = true
					}
				}
			}
			if ready {
				break
			}
			if i == interactiveTimeoutSeconds-1 {
				fmt.Fprintf(cmd.OutOrStdout(), ".\n\n")
				die("Pod not running after 90 seconds. Cancelling port-forward, tap still active.")
			}
		}
		dp, err := deploymentFromSelectors(deploymentsClient, targetService.Spec.Selector)
		if err != nil {
			return err
		}
		pod, err := kubetapPod(podsClient, dp.Name)
		if err != nil {
			return err
		}
		transport, upgrader, err := spdy.RoundTripperFor(config)
		if err != nil {
			return err
		}
		path := "/api/v1/namespaces/" + namespace + "/pods/" + pod.Name + "/portforward"
		dialer := spdy.NewDialer(upgrader,
			&http.Client{Transport: transport},
			http.MethodPost,
			&url.URL{
				Scheme: "https",
				Path:   path,
				Host:   strings.TrimLeft(config.Host, `htps:/`),
			},
		)
		fw, err := portforward.New(dialer,
			[]string{
				fmt.Sprintf("%s:%s", strconv.Itoa(kubetapProxyWebInterfacePort), strconv.Itoa(kubetapProxyWebInterfacePort)),
				fmt.Sprintf("%s:%s", "4000", strconv.Itoa(int(kubetapProxyListenPort))),
			}, stopCh, readyCh, bout, berr)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "\nPort-Forwards:\n\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  %s - http://127.0.0.1:%s\n", "mitmproxy", strconv.Itoa(int(kubetapProxyWebInterfacePort)))
		if https {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s - https://127.0.0.1:%s\n\n", targetSvcName, "4000")
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s - http://127.0.0.1:%s\n\n", targetSvcName, "4000")
		}
		if openBrowser {
			go func() {
				time.Sleep(2 * time.Second)
				_ = browser.OpenURL("http://127.0.0.1:" + strconv.Itoa(int(kubetapProxyWebInterfacePort)))
				if https {
					_ = browser.OpenURL("https://127.0.0.1:" + "4000")
				} else {
					_ = browser.OpenURL("http://127.0.0.1:" + "4000")
				}
			}()
		}
		if err := fw.ForwardPorts(); err != nil {
			return err
		}
		<-readyCh
		halt := make(chan os.Signal, 1)
		signal.Notify(halt, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
		<-halt
		return nil
	}
}

// NewUntapCommand unconditionally removes all proxies, taps, and artifacts. This is
// the inverse of NewTapCommand.
func NewUntapCommand(client kubernetes.Interface, viper *viper.Viper) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		targetSvcName := args[0]
		namespace := viper.GetString("namespace")
		if namespace == "" {
			namespace = "default"
		}
		exists, err := hasNamespace(client, namespace)
		if err != nil {
			return fmt.Errorf("error fetching namespaces: %w", err)
		}
		if !exists {
			return ErrNamespaceNotExist
		}

		deploymentsClient := client.AppsV1().Deployments(namespace)
		servicesClient := client.CoreV1().Services(namespace)
		configmapsClient := client.CoreV1().ConfigMaps(namespace)

		targetService, err := client.CoreV1().Services(namespace).Get(context.TODO(), targetSvcName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if err := destroySidecars(deploymentsClient, targetService.Spec.Selector, namespace); err != nil {
			return err
		}
		if err := untapSvc(servicesClient, targetSvcName); err != nil {
			return err
		}
		if err := destroyConfigMap(configmapsClient, targetSvcName); err != nil {
			if !errors.Is(ErrConfigMapNoMatch, err) {
				return err
			}
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Untapped Service %q\n", targetSvcName)
		return nil
	}
}

func createConfigMap(configmapClient corev1.ConfigMapInterface, proxyOpts ProxyOptions) error {
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
			Name:      kubetapConfigMapPrefix + proxyOpts.Target,
			Namespace: proxyOpts.Namespace,
			Annotations: map[string]string{
				annotationConfigMap: configMapAnnotationPrefix + proxyOpts.Target,
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

func destroyConfigMap(configmapClient corev1.ConfigMapInterface, serviceName string) error {
	if serviceName == "" {
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
			if k == annotationConfigMap && v == configMapAnnotationPrefix+serviceName {
				targetConfigMapNames = append(targetConfigMapNames, cm.Name)
			}
		}
	}
	if len(targetConfigMapNames) == 0 {
		return ErrConfigMapNoMatch
	}
	return configmapClient.Delete(context.TODO(), targetConfigMapNames[0], metav1.DeleteOptions{})
}

// deploymentFromSelectors returns a deployment given selector labels.
func deploymentFromSelectors(deploymentsClient appsv1.DeploymentInterface, selectors map[string]string) (k8sappsv1.Deployment, error) {
	var sel string
	switch len(selectors) {
	case 0:
		return k8sappsv1.Deployment{}, ErrNoSelectors
	case 1:
		for k, v := range selectors {
			sel = k + "=" + v
		}
	default:
		for k, v := range selectors {
			sel = strings.Join([]string{sel, k + "=" + v}, ",")
		}
		sel = strings.TrimLeft(sel, ",")
	}
	dpls, err := deploymentsClient.List(context.TODO(), metav1.ListOptions{
		LabelSelector: sel,
	})
	if err != nil {
		return k8sappsv1.Deployment{}, err
	}
	switch len(dpls.Items) {
	case 0:
		return k8sappsv1.Deployment{}, ErrServiceSelectorNoMatch
	case 1:
		return dpls.Items[0], nil
	default:
		return k8sappsv1.Deployment{}, ErrServiceSelectorMultiMatch
	}
}

// kubetapPod returns a kubetap pod matching a given Deployment name and Namespace.
func kubetapPod(podClient corev1.PodInterface, deploymentName string) (v1.Pod, error) {
	pods, err := podClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return v1.Pod{}, err
	}
	var p v1.Pod
	for _, pod := range pods.Items {
		anns := pod.GetAnnotations()
		if anns == nil {
			continue
		}
		for k, v := range anns {
			if k == annotationIsTapped && v == deploymentName {
				return pod, nil
			}
		}
	}
	return p, ErrKubetapPodNoMatch
}

// createSidecars edits Pod objects matching selectors to add a sidecar with the specificed image.
func createSidecars(deploymentsClient appsv1.DeploymentInterface, selectors map[string]string, image string, commandArgs []string) error {
	dpl, err := deploymentFromSelectors(deploymentsClient, selectors)
	if err != nil {
		return err
	}
	for _, c := range dpl.Spec.Template.Spec.Containers {
		if c.Name == kubetapContainerName {
			// NOTE: again, should be caught by caller...
			return ErrServiceTapped
		}
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, getErr := deploymentsClient.Get(context.TODO(), dpl.Name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		proxySidecarContainer := v1.Container{
			Name:            kubetapContainerName,
			Image:           image,
			Args:            commandArgs,
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
					Name:      kubetapConfigMapPrefix + dpl.Name,
					MountPath: "/home/mitmproxy/config/", // we store outside main dir to prevent RO problems, see below.
					// this also means that we need to wrap the official mitmproxy container.
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
		deployment.Spec.Template.Spec.Containers = append(deployment.Spec.Template.Spec.Containers, proxySidecarContainer)
		// HACK: we must set file mode to 777 to avoid pod security context problems.
		mode := int32(os.FileMode(0o777))
		deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, v1.Volume{
			Name: kubetapConfigMapPrefix + dpl.Name,
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					// this doesn't work because kube has RW issues withconfigmaps that use subpaths
					// ref: https://github.com/kubernetes/kubernetes/issues/64120
					DefaultMode: &mode,
					LocalObjectReference: v1.LocalObjectReference{
						Name: kubetapConfigMapPrefix + dpl.Name,
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
		// set annotation on pod to know what pods are tapped
		anns := deployment.Spec.Template.GetAnnotations()
		if anns == nil {
			anns = map[string]string{}
		}
		anns[annotationIsTapped] = dpl.Name
		deployment.Spec.Template.SetAnnotations(anns)
		_, updateErr := deploymentsClient.Update(context.TODO(), deployment, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return fmt.Errorf("failed to add sidecars to Deployment: %w", retryErr)
	}
	return nil
}

// destroySidecars kills the pods and allows recreation from deployment spec, but may eventually handle "untapping" more gracefully.
func destroySidecars(deploymentsClient appsv1.DeploymentInterface, selectors map[string]string, namespace string) error {
	dpl, err := deploymentFromSelectors(deploymentsClient, selectors)
	if err != nil {
		return err
	}
	if dpl.Namespace != namespace {
		// NOTE: this code path should never trigger, essentially a panic()
		return ErrDeploymentOutsideNamespace
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, getErr := deploymentsClient.Get(context.TODO(), dpl.Name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		var containersNoProxy []v1.Container
		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name != kubetapContainerName {
				containersNoProxy = append(containersNoProxy, c)
			}
		}
		deployment.Spec.Template.Spec.Containers = containersNoProxy
		var volumes []v1.Volume
		for _, v := range deployment.Spec.Template.Spec.Volumes {
			if !strings.Contains(v.Name, kubetapConfigMapPrefix) && v.Name != mitmproxyDataVolName {
				volumes = append(volumes, v)
			}
		}
		deployment.Spec.Template.Spec.Volumes = volumes
		anns := deployment.Spec.Template.GetAnnotations()
		if anns != nil {
			delete(anns, annotationIsTapped)
			deployment.Spec.Template.SetAnnotations(anns)
		}
		_, updateErr := deploymentsClient.Update(context.TODO(), deployment, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return fmt.Errorf("failed to remove sidecars from Deployment: %w", retryErr)
	}
	return nil
}

// tapSvc modifies a target port to point to a new proxy service.
func tapSvc(svcClient corev1.ServiceInterface, svcName string, targetPort int32) error {
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		svc, getErr := svcClient.Get(context.TODO(), svcName, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		anns := svc.GetAnnotations()
		// If anns is nil, it means that the target Service had no annotations.
		// BUG: this is probably safe if the comment above is true, but if it isn't
		// true this could wipe the annotations associated with a Service, which
		// would be highly undesirable....
		if anns == nil {
			anns = make(map[string]string)
		}

		if anns[annotationOriginalTargetPort] != "" {
			return ErrServiceTapped
		}
		var targetSvcPort v1.ServicePort
		var hasPort bool
		for _, sp := range svc.Spec.Ports {
			if sp.Port == targetPort {
				hasPort = true
				targetSvcPort = sp
			}
		}
		if !hasPort {
			return ErrServiceMissingPort
		}

		anns[annotationOriginalTargetPort] = targetSvcPort.TargetPort.String()
		svc.SetAnnotations(anns)

		proxySvcPort := v1.ServicePort{
			Name:       kubetapServicePortName,
			Port:       kubetapProxyWebInterfacePort,
			TargetPort: intstr.FromInt(int(kubetapProxyWebInterfacePort)),
		}
		svc.Spec.Ports = append(svc.Spec.Ports, proxySvcPort)

		// then do the swap and build a new ports list
		var servicePorts []v1.ServicePort
		for _, sp := range svc.Spec.Ports {
			if sp.Port == targetSvcPort.Port {
				if sp.Name == "" {
					sp.Name = kubetapPortName
				}
				sp.TargetPort = intstr.FromInt(kubetapProxyListenPort)
			}
			servicePorts = append(servicePorts, sp)
		}
		svc.Spec.Ports = servicePorts

		_, updateErr := svcClient.Update(context.TODO(), svc, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return fmt.Errorf("failed to tap Service: %w", retryErr)
	}
	return nil
}

// untapSvc modifies a target port point to the original service, not our proxy sidecar.
func untapSvc(svcClient corev1.ServiceInterface, svcName string) error {
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		svc, getErr := svcClient.Get(context.TODO(), svcName, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		// NOTE: it is critical to Parse here (vs FromString)
		origSvcTargetPort := intstr.Parse(svc.GetAnnotations()[annotationOriginalTargetPort])
		var servicePorts []v1.ServicePort
		for _, sp := range svc.Spec.Ports {
			if sp.Name == kubetapServicePortName {
				continue
			}
			if sp.TargetPort.IntValue() == kubetapProxyListenPort {
				if sp.Name == kubetapPortName {
					sp.Name = ""
				}
				sp.TargetPort = origSvcTargetPort
			}
			servicePorts = append(servicePorts, sp)
		}
		svc.Spec.Ports = servicePorts
		anns := svc.GetAnnotations()
		newAnns := make(map[string]string)
		for k, v := range anns {
			if k != annotationOriginalTargetPort {
				newAnns[k] = v
			}
		}
		svc.SetAnnotations(newAnns)
		_, updateErr := svcClient.Update(context.TODO(), svc, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return fmt.Errorf("failed to untap Service: %w", retryErr)
	}
	return nil
}
