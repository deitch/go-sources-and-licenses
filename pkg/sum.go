package pkg

import (
	"bufio"
	"io"
	"strings"
)

type Package struct {
	Name     string
	Version  string
	Indirect bool   // used only for go.mod
	Hash     string // used only for go.sum
}

func (p Package) String() string {
	return p.Name + "@" + p.Version
}

func ParseSum(r io.Reader) (pkgs []Package) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) != 3 {
			continue
		}
		if strings.HasSuffix(parts[1], "go.mod") {
			continue
		}
		pkgs = append(pkgs, Package{
			Name:    parts[0],
			Version: parts[1],
		})
	}
	return
}
