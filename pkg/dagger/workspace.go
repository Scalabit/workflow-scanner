package dagger

//go:generate mockgen -source=workspace.go -destination=../../mocks/workspace_mock.go -package=mocks Workspace

import "workflow-scanner/internal/dagger"

type Workspace interface {
	Source() *dagger.Directory
	GetWorkspace() *dagger.Workspace
}

type WorkspaceImpl struct {
	internal *dagger.Workspace
}

func (workspaceImpl *WorkspaceImpl) Source() *dagger.Directory {
	return workspaceImpl.internal.Source()
}

func (workspaceImpl *WorkspaceImpl) GetWorkspace() *dagger.Workspace {
	return workspaceImpl.internal
}
