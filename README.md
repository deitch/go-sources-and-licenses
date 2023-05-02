# Go Sources and Licenses

This is a simple utility to retrieve the licenses for a
go package, whether on your local filesystem or from the'
Internet.


## Licenses

For licenses, it retrieves all known licenses for the package
by downloading or reading it from your filesystem, and
looking for known license files.

The license check utilizes [the official license check from Google](github.com/google/licensecheck).

```
# download the latest version of github.com/your/package
go-sources-and-licenses licenses -m github.com/your/package

# download a specific version of github.com/your/package
go-sources-and-licenses licenses -m github.com/your/package -v v1.0.0

# read it locally
go-sources-and-licenses licenses -d /path/to/your/package
```

In addition, you can recurse through all licenses by passing
`--recursive`. In that case, in addition to reading the licenses
for the provide module, it also will read the `go.mod`, which contains the entire transitive dependency graph. and find
the licenses for all dependencies.

## Sources

For sources, it retrieves the source zip for the package
and saves it to the provided directory.

```
# download the latest version of github.com/your/package
go-sources-and-licenses sources -m github.com/your/package -o /path/to/output/

# download a specific version of github.com/your/package
go-sources-and-licenses sources -m github.com/your/package -v v1.0.0 -o /path/to/output/

# read it locally
go-sources-and-licenses sources -d /path/to/your/package -o /path/to/output/
```

The output file will be in the provided directory, and will be
named `<packagename>@<version>.zip`. If the version is not
provided, for example, if it is a local directory, then the
`@<version>` will be left off.

It can recurse through all dependencies by passing
`--recursive`. In that case, in addition to reading the sources
for the provided package, it will scan the `go.sum` and download
all dependencies into the provided output directory.

Each downloaded package will follow the naming convention
`<packagename>@<version>.zip`.