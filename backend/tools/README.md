# Tools

This directory contains a dedicated Go module with all external tools needed for the project, such as code generators, linters, etc. without changing the main dependency graph.

## Adding a tool

* navigate to the `tools` directory

```
$ cd tools
```

* add it as a tool dependency in `go.mod`

```
# go get -tool <module path>, e.g.
$ go get -tool github.com/vektra/mockery/v2
```

* clean up go modules

```
$ go mod tidy
```

* update the `install-tools` target in [tools/Makefile](Makefile)
