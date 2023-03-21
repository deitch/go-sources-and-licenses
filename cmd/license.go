package cmd

import (
	"fmt"
	"io/fs"
	"log"
	"os"
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
	var module, path, version string

	cmd := &cobra.Command{
		Use:   "license",
		Short: "List licenses",
		Long: `List licenses for a golang package, directory or go.sum file.
		Must be one of the following:
		
			licenses -m <module> -v <version>
			licenses -p <path/to/module>
		
		`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				fsys fs.FS
				err  error
			)
			if module == "" && path == "" {
				return fmt.Errorf("must specify either module or path")
			}
			if module != "" && path != "" {
				return fmt.Errorf("must specify either module or path, not both")
			}

			if module != "" {
				// must be a URL or ignore
				if !strings.Contains(module, ".") {
					log.Fatalf("module must be a URL, do not support built in modules %s", module)
				}
				if version == "" {
					log.Printf("getting latest version of %s", module)
					versions, err := pkg.GetVersions(module, proxyURL)
					if err != nil {
						log.Fatalf("failed to get versions: %v", err)
					}
					version = versions[len(versions)-1]
					log.Printf("version is %s", version)
				}
				fsys, err = pkg.GetModule(module, version, proxyURL)
				if err != nil {
					log.Fatalf("failed to get module: %v", err)
				}
			}
			if path != "" {
				fsys = os.DirFS(path)
			}
			licenses := pkg.FindLicenses(fsys)
			fmt.Println(licenses)
			return nil
		},
	}
	cmd.Flags().StringVarP(&module, "module", "m", "", "module to find and check from the Internet")
	cmd.Flags().StringVarP(&path, "dir", "d", "", "path to a golang module directory to check")
	cmd.Flags().StringVarP(&version, "version", "v", "", "version of a module to check; no meaning when providing path. For module, leave blank to get latest.")

	return cmd
}
