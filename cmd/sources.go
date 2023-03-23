package cmd

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deitch/go-sources-and-licenses/pkg"
)

type pkgInfo struct {
	module   string
	version  string
	licenses []string
	path     string
}

func sources() *cobra.Command {
	var (
		module, path, version, outpath string
		recursive                      bool
	)

	cmd := &cobra.Command{
		Use:     "sources",
		Aliases: []string{"source", "licenses", "license"},
		Short:   "Download source",
		Long: `Download sources for a golang package or directory.
		Must be one of the following:
		
			licenses -m <module> -v <version>
			licenses -d <path/to/module>
			sources -o <path/to/output.zip> -m <module> -v <version>
			sources -o <path/to/output.zip> -d <path/to/module>
		
		`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				fsys       fs.FS
				err        error
				pkgInfos   []pkgInfo
				moduleName string
			)

			switch {
			case (cmd.CalledAs() == "sources" || cmd.CalledAs() == "source") && outpath == "":
				return fmt.Errorf("must specify output path")
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
			w, filename, err := getWriter(outpath, moduleName, version)
			if err != nil {
				return fmt.Errorf("failed to create output file %s: %v", outpath, err)
			}
			defer w.Close()
			zw := zip.NewWriter(w)
			defer zw.Close()
			pkgLicenses, err := pkg.WriteToZip(fsys, zw)
			if err != nil {
				return fmt.Errorf("failed to write to zip: %v", err)
			}
			pkgInfos = append(pkgInfos, pkgInfo{module: moduleName, version: version, licenses: pkgLicenses, path: filename})

			if err != nil {
				return fmt.Errorf("failed to write to zip: %v", err)
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
					w, filename, err := getWriter(outpath, p.Name, p.Version)
					if err != nil {
						return fmt.Errorf("failed to create output file %s: %v", outpath, err)
					}
					defer w.Close()
					zw := zip.NewWriter(w)
					defer zw.Close()

					pkgLicenses, err := pkg.WriteToZip(fsys, zw)
					if err != nil {
						return fmt.Errorf("failed to write to zip: %v", err)
					}
					pkgInfos = append(pkgInfos, pkgInfo{module: p.Name, version: p.Version, licenses: pkgLicenses, path: filename})
				}
			}
			for _, p := range pkgInfos {
				fmt.Printf("%s %s %v %s\n", p.module, p.version, p.licenses, p.path)
			}

			return nil
		},
	}
	cmd.Flags().StringVarP(&module, "module", "m", "", "module to find and check from the Internet")
	cmd.Flags().StringVarP(&path, "dir", "d", "", "path to a golang module directory to check")
	cmd.Flags().StringVarP(&version, "version", "v", "", "version of a module to check; no meaning when providing path. For module, leave blank to get latest.")
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "recurse into subpackages")
	cmd.Flags().StringVarP(&outpath, "out", "o", "", "output directory for the zip files")
	return cmd
}

func cleanFilename(module, version, ext string) string {
	cleanModule := strings.Replace(module, "/", "_", -1)
	if version != "" {
		version = fmt.Sprintf("@%s", version)
	}
	return fmt.Sprintf("%s%s.%s", cleanModule, version, ext)
}

func getWriter(outpath, module, version string) (io.WriteCloser, string, error) {
	var (
		w        io.WriteCloser
		filename string
	)
	if outpath == "" {
		w = NopWriteCloser{io.Discard}
	} else {
		if err := os.MkdirAll(outpath, 0o755); err != nil {
			return nil, "", fmt.Errorf("failed to create output directory %s: %v", outpath, err)
		}
		filename = cleanFilename(module, version, "zip")
		filename = filepath.Join(outpath, filename)
		f, err := os.Create(filename)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create output file %s: %v", filename, err)
		}
		w = f
	}

	return w, filename, nil
}
