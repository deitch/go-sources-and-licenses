package cmd

import (
	"fmt"
	"log"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deitch/license-reader/pkg"
)

/*
How this works:
1. Get the URL via proxy to download the zip of a module
2. Pass the zip to the license parser
3. Find all license files in the zip
4. Parse for licenses
*/

func license() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "license",
		Short: "List licenses",
		Long: `List licenses for a golang package, directory or go.sum file.
		
			licenses <module> <version>
	
		`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			module := args[0]
			version := args[1]

			// must be a URL or ignore
			if !strings.Contains(module, ".") {
				log.Fatalf("module must be a URL, do not support built in modules %s", module)
			}
			if version == "latest" {
				log.Printf("getting latest version of %s", module)
				versions, err := pkg.GetVersions(module, proxyURL)
				if err != nil {
					log.Fatalf("failed to get versions: %v", err)
				}
				version = versions[len(versions)-1]
				log.Printf("version is %s", version)
			}
			fsys, err := pkg.GetModule(module, version, proxyURL)
			if err != nil {
				log.Fatalf("failed to get module: %v", err)
			}
			licenses := pkg.FindLicenses(module, fsys)
			fmt.Println(licenses)
			return nil
		},
	}
	return cmd
}
