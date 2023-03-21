package cmd

import (
	"archive/zip"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/deitch/license-reader/pkg"
)

/*
How this works:
1. Get the URL via proxy to download the zip of a module
2. Save the zip
*/

func sources() *cobra.Command {
	var (
		module, path, version, outpath string
		recursive                      bool
	)

	cmd := &cobra.Command{
		Use:   "sources",
		Short: "Download source",
		Long: `Download sources for a golang package or directory.
		Must be one of the following:
		
			sources -o <path/to/output.tar> -m <module> -v <version>
			sources -o <path/to/output.tar> -p <path/to/module>
		
		`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				fsys       fs.FS
				err        error
				moduleName string
			)

			switch {
			case module != "" && path != "":
				return fmt.Errorf("must specify either module or path")
			case module == "" && path == "":
				return fmt.Errorf("must specify either module or path")
			case module != "":
				moduleName = module
				fsys, err = pkg.GetModule(module, version, proxyURL)
				if err != nil {
					return fmt.Errorf("failed to get module %s: %v", module, err)
				}
			case path != "":
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

			// create the outfile
			if err := os.MkdirAll(outpath, 0o755); err != nil {
				return fmt.Errorf("failed to create output directory %s: %v", outpath, err)
			}
			filename := filepath.Join(outpath, moduleName+".tar")
			f, err := os.Create(filename)
			if err != nil {
				return fmt.Errorf("failed to create output file %s: %v", filename, err)
			}
			defer f.Close()
			zw := zip.NewWriter(f)
			defer zw.Close()
			if err := pkg.WriteToTar(fsys, zw); err != nil {
				return fmt.Errorf("failed to write to tar: %v", err)
			}

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
					filename := filepath.Join(outpath, p.Name+".tar")
					f, err := os.Create(filename)
					if err != nil {
						return fmt.Errorf("failed to create output file %s: %v", filename, err)
					}
					defer f.Close()
					zw := zip.NewWriter(f)
					defer zw.Close()

					if err := pkg.WriteToTar(fsys, zw); err != nil {
						return fmt.Errorf("failed to write to tar: %v", err)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&module, "module", "m", "", "module to find and check from the Internet")
	cmd.Flags().StringVarP(&path, "dir", "d", "", "path to a golang module directory to check")
	cmd.Flags().StringVarP(&version, "version", "v", "", "version of a module to check; no meaning when providing path. For module, leave blank to get latest.")
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "recurse into subpackages")
	cmd.Flags().StringVarP(&outpath, "out", "o", "", "output directory for the zip files")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}
