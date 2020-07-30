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
	"github.com/schollz/progressbar/v3"
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

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	kubetapContainerName         = "kubetap"
	kubetapServicePortName       = "kubetap-web"
	kubetapPortName              = "kubetap-listen"
	kubetapWebPortName           = "kubetap-web"
	kubetapProxyListenPort       = 7777
	kubetapProxyWebInterfacePort = 2244
	kubetapConfigMapPrefix       = "kubetap-target-"

	interactiveTimeoutSeconds = 90
	configMapAnnotationPrefix = "target-"

	protocolHTTP Protocol = "http"
	protocolTCP  Protocol = "tcp"
	protocolUDP  Protocol = "udp"
	protocolGRPC Protocol = "grpc"
)

var (
	ErrNamespaceNotExist          = errors.New("the provided Namespace does not exist")
	ErrServiceMissingPort         = errors.New("the target Service does not have the provided port")
	ErrServiceTapped              = errors.New("the target Service has already been tapped")
	ErrServiceSelectorNoMatch     = errors.New("the Service selector did not match any Deployments")
	ErrServiceSelectorMultiMatch  = errors.New("the Service selector matched multiple Deployments")
	ErrDeploymentOutsideNamespace = errors.New("the Service selector matched Deployment outside the specified Namespace")
	ErrSelectorsMissing           = errors.New("no selectors are set for the target Service")
	ErrConfigMapNoMatch           = errors.New("the ConfigMap list did not match any ConfigMaps")
	ErrKubetapPodNoMatch          = errors.New("a Kubetap Pod was not found")
	ErrCreateResourceMismatch     = errors.New("the created resource did not match the desired state")
	ErrDeploymentMissingPorts     = errors.New("error resolving Service port number by name from Deployment")
)

// Protocol is a supported tap method, and ultimately determines what container
// is injected as a sidecar.
type Protocol string

// Tap is a method of implementing a "Tap" for a Kubernetes cluster.
type Tap interface {
	// Sidecar produces a sidecar container to be added to a
	// Deployment.
	Sidecar(string) v1.Container

	// PatchDeployment tweaks a Deployment after a Sidecar is added
	// during the tap process.
	// Example: mitmproxy calls this function to configure the ConfigMap volume refs.
	PatchDeployment(*k8sappsv1.Deployment)

	// ReadyEnv and UnreadyEnv are used to prepare the environment
	// with resources that will be necessary for the sidecar, but do
	// not exist within a given Deployment.
	// Example: mitmproxy calls this function to apply and remove ConfigMaps for mitmproxy.
	ReadyEnv() error
	UnreadyEnv() error

	// String prints the tap method, be it mitmproxy, tcpdump, etc.
	String() string

	// Protocols returns a slice of protocols supported by the tap.
	Protocols() []Protocol
}

// ProxyOptions are options used to configure the Tap implementation.
type ProxyOptions struct {
	// Target is the target Service
	Target string `json:"target"`
	// Protocol is the protocol type, one of [http, https]
	Protocol Protocol `json:"protocol"`
	// UpstreamHTTPS should be set to true if the target is using HTTPS
	UpstreamHTTPS bool `json:"upstream_https"`
	// UpstreamPort is the listening port for the target Service
	UpstreamPort string `json:"upstream_port"`
	// Mode is the proxy mode. Only "reverse" is currently supported.
	Mode string `json:"mode"`
	// Namespace is the namespace that the Service and Deployment are in
	Namespace string `json:"namespace"`
	// Image is the proxy image to deploy as a sidecar
	Image string `json:"image"`

	// dplName tracks the current deployment target
	dplName string
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
			fmt.Fprintf(cmd.OutOrStdout(), "Tapped Services in the %s namespace:\n\n", namespace)
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

// NewTapCommand identifies a target employment through service selectors and modifies that
// deployment to add a proxy sidecar.
func NewTapCommand(client kubernetes.Interface, config *rest.Config, viper *viper.Viper) func(*cobra.Command, []string) error { //nolint: gocyclo
	return func(cmd *cobra.Command, args []string) error {
		targetSvcName := args[0]

		protocol := viper.GetString("protocol")
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
			// TODO: There is probably a way to get the default namespace from the
			// client context, but I'm not sure what that API is. Will dig
			// for that at some point.
			// BUG: "default" is not always the "correct default".
			viper.Set("namespace", "default")
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
		// Adjust default image by protocol if not manually set
		if image == defaultImageHTTP {
			switch Protocol(protocol) {
			case protocolTCP, protocolUDP:
				//TODO: make this container and remove error
				image = defaultImageRaw
				return fmt.Errorf("mode %q is currently not supported", image)
			case protocolGRPC:
				//TODO: make this container and remove error
				image = defaultImageGRPC
				return fmt.Errorf("mode %q is currently not supported", image)
			}
			viper.Set("proxyImage", image)
		}

		deploymentsClient := client.AppsV1().Deployments(namespace)
		servicesClient := client.CoreV1().Services(namespace)

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
				var err error
				targetDpl, err := deploymentFromSelectors(deploymentsClient, targetService.Spec.Selector)
				if err != nil {
					return fmt.Errorf("error resolving Deployment from Service selectors while setting proxy ports: %w", err)
				}
				for _, c := range targetDpl.Spec.Template.Spec.Containers {
					for _, p := range c.Ports {
						if p.Name == ports.TargetPort.String() {
							// Set the upstream (target) Service port
							proxyOpts.UpstreamPort = strconv.Itoa(int(p.ContainerPort))
						}
					}
				}
				if proxyOpts.UpstreamPort == "" {
					return ErrDeploymentMissingPorts
				}
			}
		}

		targetDpl, err := deploymentFromSelectors(deploymentsClient, targetService.Spec.Selector)
		if err != nil {
			return fmt.Errorf("error resolving Deployment from Service selectors: %w", err)
		}
		proxyOpts.dplName = targetDpl.Name
		// Save the target Deployment name to anchor the ConfigMap
		// to the Deployment.

		// Get a proxy based on the protocol type
		var proxy Tap
		switch Protocol(protocol) {
		case protocolTCP, protocolUDP:
			//proxy =
		case protocolGRPC:
			//proxy = abhhhhh
		default:
			// AKA, case protocolHTTP:
			proxy = NewMitmproxy(client, proxyOpts)
		}

		// Prepare the environment (configmaps, secrets, volumes, etc).
		// Nothing in ReadyEnv should modify manifests that result in
		// code running in the cluster. No Pods, no Contaniers, no ReplicaSets,
		// etc.
		if err := proxy.ReadyEnv(); err != nil {
			return err
		}

		// Setup the sidcar
		dpl, err := deploymentFromSelectors(deploymentsClient, targetService.Spec.Selector)
		if err != nil {
			return err
		}
		// set the Deployment name so the configmap tracks a specific Deployment

		sidecar := proxy.Sidecar(dpl.Name)
		sidecar.Image = image
		sidecar.Args = commandArgs

		// Apply the Deployment configuration
		retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			dpl.Spec.Template.Spec.Containers = append(dpl.Spec.Template.Spec.Containers, sidecar)
			proxy.PatchDeployment(&dpl)
			// set annotation on pod to know what pods are tapped
			anns := dpl.Spec.Template.GetAnnotations()
			if anns == nil {
				anns = map[string]string{}
			}
			anns[annotationIsTapped] = dpl.Name
			dpl.Spec.Template.SetAnnotations(anns)
			_, updateErr := deploymentsClient.Update(context.TODO(), &dpl, metav1.UpdateOptions{})
			return updateErr
		})
		if retryErr != nil {
			fmt.Fprintln(cmd.OutOrStdout(), "Error modifying Deployment, reverting tap...")
			_ = NewUntapCommand(client, viper)(cmd, args)
			return fmt.Errorf("failed to add sidecars to Deployment: %w", retryErr)
		}

		// Tap the Service to redirect the incoming traffic to our proxy, which is configured to redirect
		// to the original port.
		if err := tapSvc(servicesClient, targetSvcName, targetSvcPort); err != nil {
			fmt.Fprintln(cmd.OutOrStdout(), "Error modifying Service, reverting tap...")
			_ = NewUntapCommand(client, viper)(cmd, args)
			return err
		}

		if !portForward {
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintf(cmd.OutOrStdout(), "Port %d of Service %q has been tapped!\n\n", targetSvcPort, targetSvcName)
			fmt.Fprintf(cmd.OutOrStdout(), "You can access the proxy web interface at http://127.0.0.1:2244\n")
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
		s := make(chan struct{})
		defer close(s)
		go func() {
			// Skip the first few checks to give pods time to come up.
			// Race: If the first few cycles are not skipped, the condition status may be "Ready".
			time.Sleep(5 * time.Second)
			s <- struct{}{}
		}()
		bar := progressbar.NewOptions(interactiveTimeoutSeconds,
			progressbar.OptionThrottle(100*time.Millisecond),
			progressbar.OptionClearOnFinish(),
			progressbar.OptionSetPredictTime(false),
			progressbar.OptionSetWriter(cmd.OutOrStderr()),
			progressbar.OptionSetWidth(30),
			progressbar.OptionSetDescription("Waiting for Pod containers to become ready..."),
			progressbar.OptionOnCompletion(func() {
				fmt.Fprintf(cmd.OutOrStderr(), "\n")
			}),
		)
		var ready bool
		for i := 0; i < interactiveTimeoutSeconds; i++ {
			_ = bar.Add(1)
			if ready {
				_ = bar.Finish()
				break
			}
			time.Sleep(1 * time.Second)
			select {
			case <-time.After(1 * time.Nanosecond):
				// if not ready this cycle, abort
				continue
			case <-s:
				dp, err := deploymentFromSelectors(deploymentsClient, targetService.Spec.Selector)
				if err != nil {
					return err
				}
				pod, err := kubetapPod(podsClient, dp.Name)
				if err != nil {
					return err
				}
				for _, cond := range pod.Status.Conditions {
					if cond.Type == "ContainersReady" {
						if cond.Status == "True" {
							ready = true
						}
					}
				}
				go func() {
					s <- struct{}{}
				}()
			}
		}
		if !ready {
			fmt.Fprintf(cmd.OutOrStdout(), ".\n\n")
			die("Pod not running after 90 seconds. Cancelling port-forward, tap still active.")
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
				Host:   strings.TrimPrefix(strings.TrimPrefix(config.Host, `http://`), `https://`),
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
		fmt.Fprintf(cmd.OutOrStdout(), "  %s - http://127.0.0.1:%s\n", proxy.String(), strconv.Itoa(int(kubetapProxyWebInterfacePort)))
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

		targetService, err := client.CoreV1().Services(namespace).Get(context.TODO(), targetSvcName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		dpl, err := deploymentFromSelectors(deploymentsClient, targetService.Spec.Selector)
		if err != nil {
			return err
		}
		if dpl.Namespace != namespace {
			panic(ErrDeploymentOutsideNamespace)
		}

		proxy := NewMitmproxy(client, ProxyOptions{
			Namespace: namespace,
			Target:    targetSvcName,
			dplName:   dpl.Name,
		})

		if err := proxy.UnreadyEnv(); err != nil {
			// both error types below can be thrown
			if !errors.Is(ErrConfigMapNoMatch, err) {
				return err
			}
		}

		retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Explicitly re-fetch the deployment to reduce the chance of having a race
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
				if !strings.HasPrefix(v.Name, "kubetap") {
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
		if err := untapSvc(servicesClient, targetSvcName); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Untapped Service %q\n", targetSvcName)
		return nil
	}
}

// deploymentFromSelectors returns a deployment given selector labels.
func deploymentFromSelectors(deploymentsClient appsv1.DeploymentInterface, selectors map[string]string) (k8sappsv1.Deployment, error) {
	var sel string
	switch len(selectors) {
	case 0:
		return k8sappsv1.Deployment{}, ErrSelectorsMissing
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
