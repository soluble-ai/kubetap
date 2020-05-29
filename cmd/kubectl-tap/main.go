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
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	// Set by CI
	version = "dev"
	date    = "not_set" //nolint: gochecknoglobals
	commit  = "not_set" //nolint: gochecknoglobals
)

const (
	annotationOriginalTargetPort = "kubetap.io/original-port"
	annotationConfigMap          = "kubetap.io/proxy-config"
	annotationIsTapped           = "kubetap.io/tapped"

	defaultImageHTTP = "gcr.io/soluble-oss/kubetap-mitmproxy:latest"
	defaultImageRaw  = "gcr.io/soluble-oss/kubetap-raw:latest"
	defaultImageGRPC = "gcr.io/soluble-oss/kubetap-grpc:latest"
)

// die exit the program, printing the error.
func die(args ...interface{}) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}

func main() {
	exiter := &Exit{}
	rootCmd := NewRootCmd(exiter)

	kubernetesConfigFlags := genericclioptions.NewConfigFlags(false)
	kubernetesConfigFlags.AddFlags(rootCmd.PersistentFlags())

	config, err := kubernetesConfigFlags.ToRESTConfig()
	if err != nil {
		die(err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		die(err)
	}
	if err := viper.BindPFlags(rootCmd.PersistentFlags()); err != nil {
		die(err)
	}

	versionCmd := NewVersionCmd()
	onCmd := NewOnCmd(client, config)
	offCmd := NewOffCmd(client)
	listCmd := NewListCmd(client)

	onCmd.Flags().StringP("port", "p", "", "target Service port")
	onCmd.Flags().StringP("image", "i", defaultImageHTTP, "image to run in proxy container")
	onCmd.Flags().Bool("https", false, "enable if target listener uses HTTPS")
	onCmd.Flags().String("command-args", "mitmweb", "specify command arguments for the proxy sidecar container")
	onCmd.Flags().Bool("port-forward", false, "enable to automatically kubctl port-forward to services")
	onCmd.Flags().Bool("browser", false, "enable to open browser windows to service and proxy. Also enables --port-forward")
	onCmd.Flags().String("protocol", "http", "specify a protocol. Supported protocols: [ http ]")

	rootCmd.AddCommand(versionCmd, onCmd, offCmd, listCmd)

	if err := rootCmd.Execute(); err != nil {
		exiter.Exit(1)
	}
}

// bindTapFlags is a workaround for https://github.com/spf13/viper/issues/233
func bindTapFlags(cmd *cobra.Command, _ []string) error {
	if err := viper.BindPFlag("proxyPort", cmd.Flags().Lookup("port")); err != nil {
		return err
	}
	if err := viper.BindPFlag("proxyImage", cmd.Flags().Lookup("image")); err != nil {
		return err
	}
	if err := viper.BindPFlag("https", cmd.Flags().Lookup("https")); err != nil {
		return err
	}
	if err := viper.BindPFlag("commandArgs", cmd.Flags().Lookup("command-args")); err != nil {
		return err
	}
	if err := viper.BindPFlag("portForward", cmd.Flags().Lookup("port-forward")); err != nil {
		return err
	}
	if err := viper.BindPFlag("browser", cmd.Flags().Lookup("browser")); err != nil {
		return err
	}
	if err := viper.BindPFlag("protocol", cmd.Flags().Lookup("protocol")); err != nil {
		return err
	}
	return nil
}

func NewRootCmd(e Exiter) *cobra.Command {
	return &cobra.Command{
		// HACK: there is a "bug" in cobra's handling of Use strings with spaces, so the space
		// below in the Use field isn't a true space -- it's a unicode Em space.
		// Also, if you can't see the space below prominently, you need to change your editor settings
		// to reveal Unicode characters. Otherwise, you're likely to PR some malicious code with a unicode
		// domain at some point.
		Use:   "kubectl tap",
		Short: "kubetap",
		Example: ` Create a tap for a new Service:
   kubectl tap on -n demo -p443 --https sample-service",

 List active taps in all namespaces:
   kubectl tap list

 Disable the tap with the off command:
   kubectl tap off -n demo sample-service`,
		Long: `kubetap - proxy Services in Kubernetes with ease.

 More information is available at the project website:
   https://soluble-ai.github.io/kubetap/

 Created and maintained by Soluble:
   https://www.soluble.ai/
`,
		Run: func(cmd *cobra.Command, _ []string) {
			// NOTE: explicitly print out usage here, but overriding it for subcommands by way
			// of SilenceUsage: true
			_ = cmd.Usage()
			e.Exit(64) // EX_USAGE
		},
		SilenceUsage: true,
	}
}

func NewOnCmd(client kubernetes.Interface, config *rest.Config) *cobra.Command {
	return &cobra.Command{
		Use:     "on",
		Short:   "Tap a Service",
		Example: "kubectl tap on -n my-namespace -p443 --https my-sample-service",
		PreRunE: bindTapFlags,
		RunE:    NewTapCommand(client, config, viper.GetViper()),
		Args:    cobra.ExactArgs(1),
	}
}

func NewOffCmd(client kubernetes.Interface) *cobra.Command {
	return &cobra.Command{
		Use:     "off",
		Short:   "Untap a Service",
		Example: "kubectl tap off -n my-namespace my-sample-service",
		RunE:    NewUntapCommand(client, viper.GetViper()),
		Args:    cobra.ExactArgs(1),
	}
}

func NewListCmd(client kubernetes.Interface) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List tapped Services",
		RunE:  NewListCommand(client, viper.GetViper()),
	}
}

func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version of kubectl-tap",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "version: %s, commit: %s, built at: %s\n", version, commit, date)
		},
	}
}

// Exiter exits the program, calling os.Exit(code), nothing more.
type Exiter interface {
	Exit(code int)
}

type Exit struct{}

func (e *Exit) Exit(code int) {
	os.Exit(code)
}
