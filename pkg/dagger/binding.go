package dagger

//go:generate mockgen -source=binding.go -destination=../../mocks/binding_mock.go -package=mocks Binding

import (
	"context"
	"workflow-scanner/internal/dagger"
)

type Binding interface {
	AsString(ctx context.Context) (string, error)
	AsDirectory() *dagger.Directory
	AsWorkspace() Workspace
}

type BindingImpl struct {
	internal *dagger.Binding
}

func (bindingImpl *BindingImpl) AsString(ctx context.Context) (string, error) {
	return bindingImpl.internal.AsString(ctx)
}

func (bindingImpl *BindingImpl) AsDirectory() *dagger.Directory {
	return bindingImpl.internal.AsDirectory()
}

func (bindingImpl *BindingImpl) AsWorkspace() Workspace {
	return &WorkspaceImpl{internal: bindingImpl.internal.AsWorkspace()}
}
