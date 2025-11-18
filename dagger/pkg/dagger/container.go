package dagger

import (
	"context"
	"dagger/workflow-scanner/internal/dagger"
)

type Container interface {
	Directory(path string, opts ...dagger.ContainerDirectoryOpts) *dagger.Directory
	From(address string) *dagger.Container
	Stdout(ctx context.Context) (string, error)
	WithExec(args []string, opts ...dagger.ContainerWithExecOpts) *dagger.Container
	WithDirectory(path string, source *dagger.Directory, opts ...dagger.ContainerWithDirectoryOpts) *dagger.Container
	WithWorkdir(path string, opts ...dagger.ContainerWithWorkdirOpts) *dagger.Container
}
