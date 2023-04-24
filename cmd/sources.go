package cmd

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/deitch/go-sources-and-licenses/pkg"
)

const (
	modFile         = "go.mod"
	defaultTemplate = `{{.Module}} {{.Version}} {{.Licenses}} {{.Path}}`
)

type pkgInfo struct {
	Module   string
	Version  string
	Licenses []string
	Path     string
}

func sources() *cobra.Command {
	var (
		module, path, version, outpath, format string
		recursive, find                        bool
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
			sources -o <path/to/output.zip> -d <path/to/module> -f
		
		`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				fsys       fs.FS
				err        error
				pkgInfos   []pkgInfo
				moduleName string
			)

			tmpl, err := template.New("sources").Parse(format)
			if err != nil {
				return fmt.Errorf("failed to parse template: %v", err)
			}

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
				log.Debugf("writing module %s version %s from direct package", moduleName, version)
				added, err := writeModule(outpath, version, fsys, recursive)
				if err != nil {
					return err
				}
				pkgInfos = append(pkgInfos, added...)
			case path != "" && !find:
				fsys = os.DirFS(path)
				log.Debugf("writing module %s version %s from directory %s", moduleName, version, path)
				added, err := writeModule(outpath, version, fsys, recursive)
				if err != nil {
					return err
				}
				pkgInfos = append(pkgInfos, added...)
			case path != "" && find:
				log.Debugf("find enabled based at %s", path)
				fsys = os.DirFS(path)
				fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
					if err != nil && !errors.Is(err, io.EOF) {
						return fmt.Errorf("failed to walk %s: %v", path, err)
					}
					// we only are looking for directories with go.mod in them
					if !strings.HasSuffix(path, modFile) {
						return nil
					}
					sub, err := fs.Sub(fsys, filepath.Dir(path))
					if err != nil {
						return fmt.Errorf("failed to get subdirectory %s: %v", path, err)
					}
					log.Debugf("writing module %s version %s from inside directory %s", moduleName, version, path)
					added, err := writeModule(outpath, version, sub, recursive)
					if err != nil {
						return err
					}
					pkgInfos = append(pkgInfos, added...)
					return nil
				})
			}

			for _, p := range pkgInfos {
				tmpl.Execute(os.Stdout, p)
				fmt.Println()
			}

			return nil
		},
	}
	cmd.Flags().StringVarP(&module, "module", "m", "", "module to find and check from the Internet")
	cmd.Flags().StringVarP(&path, "dir", "d", "", "path to a golang module directory to check")
	cmd.Flags().StringVarP(&version, "version", "v", "", "version of a module to check; no meaning when providing path. For module, leave blank to get latest.")
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "recurse into subpackages")
	cmd.Flags().BoolVarP(&find, "find", "f", false, "find recursively within the provided directory, equivalent of 'find <dir> -name go.mod'; useful only with --dir, ignored otherwise")
	cmd.Flags().StringVarP(&outpath, "out", "o", "", "output directory for the zip files")
	cmd.Flags().StringVar(&format, "template", defaultTemplate, "output template to use. Available fields are: .Module, .Version, .Licenses, .Path")
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

func writeModule(outpath, version string, fsys fs.FS, recursive bool) (pkgInfos []pkgInfo, err error) {
	f, err := fsys.Open(modFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %v", modFile, err)
	}
	defer f.Close()
	// read the package name
	mod, err := pkg.ParseMod(f)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %v", modFile, err)
	}
	// create the outfile
	w, filename, err := getWriter(outpath, mod.Name, version)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file %s: %v", outpath, err)
	}
	defer w.Close()
	zw := zip.NewWriter(w)
	defer zw.Close()
	pkgLicenses, err := pkg.WriteToZip(fsys, zw)
	if err != nil {
		return nil, fmt.Errorf("failed to write to zip: %v", err)
	}
	pkgInfos = append(pkgInfos, pkgInfo{Module: mod.Name, Version: version, Licenses: pkgLicenses, Path: filename})

	if err != nil {
		return nil, fmt.Errorf("failed to write to zip: %v", err)
	}

	if recursive {
		sumFile := "go.sum"
		f, err := fsys.Open(sumFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open %s: %v", sumFile, err)
		}
		defer f.Close()
		pkgs := pkg.ParseSum(f)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %v", sumFile, err)
		}
		for _, p := range pkgs {
			fsys, err = pkg.GetModule(p.Name, p.Version, proxyURL)
			if err != nil {
				return nil, fmt.Errorf("failed to get module %s: %v", p.Name, err)
			}
			w, filename, err := getWriter(outpath, p.Name, p.Version)
			if err != nil {
				return nil, fmt.Errorf("failed to create output file %s: %v", outpath, err)
			}
			defer w.Close()
			zw := zip.NewWriter(w)
			defer zw.Close()

			pkgLicenses, err := pkg.WriteToZip(fsys, zw)
			if err != nil {
				return nil, fmt.Errorf("failed to write to zip: %v", err)
			}
			pkgInfos = append(pkgInfos, pkgInfo{Module: p.Name, Version: p.Version, Licenses: pkgLicenses, Path: filename})
		}
	}
	return
}
