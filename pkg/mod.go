package pkg

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

type Replace struct {
	Old string
	New Package
}
type ModFile struct {
	Name      string
	GoVersion string
	Requires  []Package
	Replace   []Replace
}

func ParseMod(r io.Reader) (*ModFile, error) {
	var m ModFile
	sc := bufio.NewScanner(r)
	var inRequire, inReplace bool

	for sc.Scan() {
		line := sc.Text()
		// ignore comments
		if strings.HasPrefix(line, "//") {
			continue
		}
		parts := strings.Fields(line)
		switch {
		case len(parts) == 0:
			continue
		case parts[0] == "module":
			if m.Name != "" {
				return nil, fmt.Errorf("invalid go.mod: multiple module lines")
			}
			m.Name = parts[1]
		case parts[0] == "go":
			if m.GoVersion != "" {
				return nil, fmt.Errorf("invalid go.mod: multiple go lines")
			}
			m.GoVersion = parts[1]
		case parts[0] == "require":
			if inRequire {
				return nil, fmt.Errorf("invalid go.mod: nested require blocks")
			}
			if len(parts) < 2 {
				return nil, fmt.Errorf("invalid go.mod: standalone require on a line")
			}
			if parts[1] == "(" {
				inRequire = true
				continue
			}
			entry, err := requireEntry(parts[1:])
			if err != nil {
				return nil, err
			}
			m.Requires = append(m.Requires, entry)
		case parts[0] == "replace":
			if inReplace {
				return nil, fmt.Errorf("invalid go.mod: nested replace blocks")
			}
			if len(parts) < 2 {
				return nil, fmt.Errorf("invalid go.mod: standalone replace on a line")
			}
			if parts[1] == "(" {
				inReplace = true
				continue
			}
			entry, err := replaceEntry(parts[1:])
			if err != nil {
				return nil, err
			}
			m.Replace = append(m.Replace, entry)
		case parts[0] == ")":
			switch {
			case inRequire:
				inRequire = false
			case inReplace:
				inReplace = false
			default:
				return nil, fmt.Errorf("invalid go.mod: unexpected closing paren")
			}
		default:
			// just a regular line
			switch {
			case inRequire:
				entry, err := requireEntry(parts)
				if err != nil {
					return nil, err
				}
				m.Requires = append(m.Requires, entry)
			case inReplace:
				entry, err := replaceEntry(parts)
				if err != nil {
					return nil, err
				}
				m.Replace = append(m.Replace, entry)
			default:
				return nil, fmt.Errorf("invalid go.mod: unexpected line")
			}
		}
	}
	return &m, nil
}

func requireEntry(line []string) (p Package, err error) {
	if len(line) < 2 {
		return Package{}, fmt.Errorf("invalid go.mod: standalone require on a line")
	}
	entry := Package{
		Name:    line[0],
		Version: line[1],
	}
	if len(line) > 3 && strings.HasSuffix(line[len(line)-1], "indirect") {
		entry.Indirect = true
	}
	return entry, nil
}

func replaceEntry(line []string) (r Replace, err error) {
	if len(line) < 3 || line[1] != "=>" {
		return r, fmt.Errorf("invalid go.mod: invalid replace line")
	}
	entry := Replace{
		Old: line[0],
		New: Package{
			Name: line[2],
		},
	}
	if len(line) > 3 {
		entry.New.Version = line[3]
	}
	return
}
