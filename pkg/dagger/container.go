package dagger

//go:generate mockgen -source=container.go -destination=../../mocks/container_mock.go -package=mocks Container

import (
	"context"
	"dagger/workflow-scanner/internal/dagger"
)

type Container interface {
	Directory(path string, opts ...dagger.ContainerDirectoryOpts) *dagger.Directory
	From(address string) Container
	Stdout(ctx context.Context) (string, error)
	WithExec(args []string, opts ...dagger.ContainerWithExecOpts) Container
	WithDirectory(path string, source *dagger.Directory, opts ...dagger.ContainerWithDirectoryOpts) Container
	WithWorkdir(path string, opts ...dagger.ContainerWithWorkdirOpts) Container
}

type ContainerImpl struct {
	internal *dagger.Container
}

func (containerImpl *ContainerImpl) Directory(path string, opts ...dagger.ContainerDirectoryOpts) *dagger.Directory {
	return containerImpl.internal.Directory(path, opts...)
}

func (containerImpl *ContainerImpl) From(address string) Container {
	return &ContainerImpl{internal: containerImpl.internal.From(address)}
}

func (containerImpl *ContainerImpl) Stdout(ctx context.Context) (string, error) {
	return containerImpl.internal.Stdout(ctx)
}

func (containerImpl *ContainerImpl) WithExec(args []string, opts ...dagger.ContainerWithExecOpts) Container {
	return &ContainerImpl{internal: containerImpl.internal.WithExec(args, opts...)}
}

func (containerImpl *ContainerImpl) WithDirectory(path string, source *dagger.Directory, opts ...dagger.ContainerWithDirectoryOpts) Container {
	return &ContainerImpl{internal: containerImpl.internal.WithDirectory(path, source, opts...)}
}

func (containerImpl *ContainerImpl) WithWorkdir(path string, opts ...dagger.ContainerWithWorkdirOpts) Container {
	return &ContainerImpl{internal: containerImpl.internal.WithWorkdir(path, opts...)}
}
