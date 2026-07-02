## Cells

Cells are the equivalent of buildpacks. They create "cells" for services that encapsulate 
their dependencies and runtime requirement. They isolate the service from the outside 
world and provide a single entry point via http port 8080.

In the event a service does not have a http server e.g shell scripts we start one for it.

## Go 1.26 service cell

The Kubernetes runtime default image remains `micro/cells:v3` for existing services.
Build the Go 1.26 service cell from the existing v3 loader under a new tag:

```sh
CELL_DIR=v3 CELL_TAG=v3-go1.26 ./build.sh
```

Deploy Go 1.26 SDK services by selecting the new image explicitly:

```sh
micro run <source> --image micro/cells:v3-go1.26
```
