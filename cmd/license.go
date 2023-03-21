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

type license struct {
	module   string
	version  string
	licenses []string
}

func licenses() *cobra.Command {
	var (
		module, path, version string
		recursive             bool
	)

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
				fsys       fs.FS
				err        error
				licenses   []license
				moduleName string
			)
			if module == "" && path == "" {
				return fmt.Errorf("must specify either module or path")
			}
			if module != "" && path != "" {
				return fmt.Errorf("must specify either module or path, not both")
			}

			if module != "" {
				// must be a valid module or ignore
				if !strings.Contains(module, ".") {
					return fmt.Errorf("module must be a valid go module, does not support built in modules %s", module)
				}
				moduleName = module
				if version == "" {
					log.Printf("getting latest version of %s", module)
					versions, err := pkg.GetVersions(module, proxyURL)
					if err != nil {
						return fmt.Errorf("failed to get versions: %v", err)
					}
					version = versions[len(versions)-1]
					log.Printf("version is %s", version)
				}
				fsys, err = pkg.GetModule(module, version, proxyURL)
				if err != nil {
					return fmt.Errorf("failed to get module %s: %v", module, err)
				}
			}
			if path != "" {
				fsys = os.DirFS(path)
				modFile := "go.mod"
				f, err := fsys.Open(modFile)
				if err != nil {
					return fmt.Errorf("failed to open %s: %v", modFile, err)
				}
				defer f.Close()
				// read the package name
				mod, err := pkg.ParseMod(f)
				if err != nil {
					return fmt.Errorf("failed to parse %s: %v", modFile, err)
				}
				moduleName = mod.Name
			}
			pkgLicenses := pkg.FindLicenses(fsys)
			licenses = append(licenses, license{module: moduleName, version: version, licenses: pkgLicenses})

			if recursive {
				sumFile := "go.sum"
				f, err := fsys.Open(sumFile)
				if err != nil {
					return fmt.Errorf("failed to open %s: %v", sumFile, err)
				}
				defer f.Close()
				pkgs := pkg.ParseSum(f)
				if err != nil {
					return fmt.Errorf("failed to parse %s: %v", sumFile, err)
				}
				for _, p := range pkgs {
					fsys, err = pkg.GetModule(p.Name, p.Version, proxyURL)
					if err != nil {
						return fmt.Errorf("failed to get module %s: %v", p.Name, err)
					}
					pkgLicenses := pkg.FindLicenses(fsys)
					licenses = append(licenses, license{module: p.Name, version: p.Version, licenses: pkgLicenses})
				}
			}
			for _, l := range licenses {
				fmt.Printf("%s %s %v\n", l.module, l.version, l.licenses)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&module, "module", "m", "", "module to find and check from the Internet")
	cmd.Flags().StringVarP(&path, "dir", "d", "", "path to a golang module directory to check")
	cmd.Flags().StringVarP(&version, "version", "v", "", "version of a module to check; no meaning when providing path. For module, leave blank to get latest.")
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "recurse into subpackages")
	return cmd
}
