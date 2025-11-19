package dagger

import (
	"dagger/workflow-scanner/internal/dagger"
)

type Client interface {
	Container(opts ...dagger.ContainerOpts) Container
	Env(opts ...dagger.EnvOpts) Env
	Workspace(source *dagger.Directory) Workspace
	CurrentModule() CurrentModule
	LLM(opts ...dagger.LLMOpts) LLM
}
