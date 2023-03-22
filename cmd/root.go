package cmd

import (
	"github.com/spf13/cobra"
)

const (
	defaultProxyURL = "https://proxy.golang.org"
)

var proxyURL string

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "license-reader",
		DisableAutoGenTag: true,
		SilenceUsage:      true,
	}

	cmd.AddCommand(sources())

	cmd.PersistentFlags().StringVarP(&proxyURL, "proxy", "p", defaultProxyURL, "proxy URL to use")
	return cmd
}
