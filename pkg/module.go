package pkg

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/licensecheck"
)

const (
	coverageThreshold  = 75
	unknownLicenseType = "UNKNOWN"
)

/*
How this works:
1. Get the URL via proxy to download the zip of a module
2. Pass the zip to the license parser
3. Find all license files in the zip
4. Parse for licenses
*/

func GetModule(module, version, proxy string) (fs.FS, error) {
	// first see if we have it locally
	goPath := os.Getenv("GOPATH")
	if goPath != "" {
		modPath := filepath.Join(goPath, "pkg", "mod", fmt.Sprintf("%s@%s", module, version))
		if fi, err := os.Stat(modPath); err == nil && fi != nil && fi.IsDir() {
			log.Printf("found module locally at %s", modPath)
			modFS := os.DirFS(modPath)
			return modFS, nil
		}
	}

	// we could not get it locally, so get it from the proxy

	// get the module zip
	resp, err := http.Get(fmt.Sprintf("%s/%s/@v/%s.zip", proxy, module, version))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get module zip: %s", resp.Status)
	}
	// read the zip
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	log.Print("found module via proxy")
	return zip.NewReader(bytes.NewReader(b), resp.ContentLength)
}

func GetVersions(module, proxy string) ([]string, error) {
	resp, err := http.Get(fmt.Sprintf("%s/%s/@v/list", proxy, module))
	if err != nil {
		return nil, err
	}
	var versions []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		versions = append(versions, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return versions, nil

}

func FindLicenses(module string, fsys fs.FS) []string {
	var (
		licenses []string
		isVendor bool
	)
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		filename := filepath.Base(p)
		// ignore any tat are not a known filetype
		if _, ok := fileNames[filename]; !ok {
			return nil
		}
		// make sure it is not in a vendored path
		parts := strings.Split(filepath.Dir(p), string(filepath.Separator))
		for _, part := range parts {
			if part == "vendor" {
				isVendor = true
				break
			}
		}
		if isVendor {
			return nil
		}
		// read the file contents
		rc, err := fsys.Open(p)
		if err != nil {
			return nil
		}
		defer rc.Close()
		contents, err := io.ReadAll(rc)
		if err != nil {
			return nil
		}
		cov := licensecheck.Scan(contents)

		if cov.Percent < float64(coverageThreshold) {
			licenses = append(licenses, unknownLicenseType)
		}
		for _, m := range cov.Match {
			licenses = append(licenses, m.ID)
		}
		return nil
	})
	return licenses
}
