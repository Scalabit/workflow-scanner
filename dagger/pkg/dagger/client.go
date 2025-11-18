package dagger

import (
	"dagger/workflow-scanner/internal/dagger"
)

type Client interface {
	Container(opts ...dagger.ContainerOpts) *dagger.Container
}
