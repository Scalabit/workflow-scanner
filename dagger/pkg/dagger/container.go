package dagger

import (
	"context"
	"dagger/workflow-scanner/internal/dagger"
)

type Container interface {
	Directory(path string, opts ...dagger.ContainerDirectoryOpts) Directory
	From(address string) Container
	Stdout(ctx context.Context) (string, error)
	WithExec(args []string, opts ...dagger.ContainerWithExecOpts) Container
	WithDirectory(path string, source Directory, opts ...dagger.ContainerWithDirectoryOpts) Container
	WithWorkdir(path string, opts ...dagger.ContainerWithWorkdirOpts) Container
}
