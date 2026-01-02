package dagger

//go:generate mockgen -source=client.go -destination=../../mocks/client_mock.go -package=mocks Client

import (
	"workflow-scanner/internal/dagger"
)

type Client interface {
	Container(opts ...dagger.ContainerOpts) Container
	Env(opts ...dagger.EnvOpts) Env
	Workspace(source *dagger.Directory) Workspace
	CurrentModule() CurrentModule
	LLM(opts ...dagger.LLMOpts) LLM
	SetSecret(name, plaintext string) *dagger.Secret
}

type ClientImpl struct {
	internal *dagger.Client
}

func (clientImpl *ClientImpl) SetSecret(name, plaintext string) *dagger.Secret {
	return clientImpl.internal.SetSecret(name, plaintext)
}

func NewClient(client *dagger.Client) Client {
	return &ClientImpl{
		internal: client,
	}
}

func (clientImpl *ClientImpl) Container(opts ...dagger.ContainerOpts) Container {
	return &ContainerImpl{internal: clientImpl.internal.Container(opts...)}
}

func (clientImpl *ClientImpl) Env(opts ...dagger.EnvOpts) Env {
	return &EnvImpl{internal: clientImpl.internal.Env(opts...)}
}

func (clientImpl *ClientImpl) Workspace(source *dagger.Directory) Workspace {
	return &WorkspaceImpl{internal: clientImpl.internal.Workspace(source)}
}

func (clientImpl *ClientImpl) CurrentModule() CurrentModule {
	return &CurrentModuleImpl{internal: clientImpl.internal.CurrentModule()}
}

func (clientImpl *ClientImpl) LLM(opts ...dagger.LLMOpts) LLM {
	return &LLMImpl{internal: clientImpl.internal.LLM(opts...)}
}
