package cmd

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	defaultProxyURL = "https://proxy.golang.org"
)

var (
	proxyURL string
	debug    bool
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "license-reader",
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if debug {
				logrus.SetLevel(logrus.DebugLevel)
			}
			return nil
		},
	}

	cmd.AddCommand(sources())

	cmd.PersistentFlags().StringVarP(&proxyURL, "proxy", "p", defaultProxyURL, "proxy URL to use")
	cmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging")
	return cmd
}
