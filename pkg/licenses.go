package pkg

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"

	"github.com/google/licensecheck"
)

// all of these taken from https://github.com/golang/pkgsite/blob/8996ff632abee854aef1b764ca0501f262f8f523/internal/licenses/licenses.go#L338
// which unfortunately is not exported. But fortunately is under BSD-style license.

var (
	FileNames = []string{
		"COPYING",
		"COPYING.md",
		"COPYING.markdown",
		"COPYING.txt",
		"LICENCE",
		"LICENCE.md",
		"LICENCE.markdown",
		"LICENCE.txt",
		"LICENSE",
		"LICENSE.md",
		"LICENSE.markdown",
		"LICENSE.txt",
		"LICENSE-2.0.txt",
		"LICENCE-2.0.txt",
		"LICENSE-APACHE",
		"LICENCE-APACHE",
		"LICENSE-APACHE-2.0.txt",
		"LICENCE-APACHE-2.0.txt",
		"LICENSE-MIT",
		"LICENCE-MIT",
		"LICENSE.MIT",
		"LICENCE.MIT",
		"LICENSE.code",
		"LICENCE.code",
		"LICENSE.docs",
		"LICENCE.docs",
		"LICENSE.rst",
		"LICENCE.rst",
		"MIT-LICENSE",
		"MIT-LICENCE",
		"MIT-LICENSE.md",
		"MIT-LICENCE.md",
		"MIT-LICENSE.markdown",
		"MIT-LICENCE.markdown",
		"MIT-LICENSE.txt",
		"MIT-LICENCE.txt",
		"MIT_LICENSE",
		"MIT_LICENCE",
		"UNLICENSE",
		"UNLICENCE",
	}
)

var licenseFileNames map[string]bool

func init() {
	licenseFileNames = make(map[string]bool)
	for _, name := range FileNames {
		licenseFileNames[name] = true
	}
}

func licenseChecker(r io.ReadCloser, p string) io.ReadCloser {
	filename := filepath.Base(p)
	// ignore any that are not a known filetype
	if _, ok := licenseFileNames[filename]; !ok {
		return r
	}
	// make sure it is not in a vendored path
	var isVendor bool
	parts := strings.Split(filepath.Dir(p), string(filepath.Separator))
	for _, part := range parts {
		if part == "vendor" {
			isVendor = true
			break
		}
	}
	if isVendor {
		return r
	}
	// it matched, and is not in vendor; create a TeeWriter and a reader to process it
	var buf bytes.Buffer
	tr := io.TeeReader(r, &buf)

	return &licenseReader{Reader: tr, buf: &buf}
}

type licenseReader struct {
	io.Reader
	buf      *bytes.Buffer
	licenses []string
}

func (l *licenseReader) Close() error {
	// process the data
	contents := l.buf.Bytes()
	cov := licensecheck.Scan(contents)

	if cov.Percent < float64(coverageThreshold) {
		l.licenses = append(l.licenses, unknownLicenseType)
	}
	for _, m := range cov.Match {
		l.licenses = append(l.licenses, m.ID)
	}
	return nil
}
