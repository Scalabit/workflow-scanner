package dagger

import (
	"dagger/workflow-scanner/internal/dagger"
)

type Client interface {
	Container(opts ...dagger.ContainerOpts) *dagger.Container
	Env(opts ...dagger.EnvOpts) *dagger.Env
	Workspace(source *dagger.Directory) *dagger.Workspace
	CurrentModule() *dagger.CurrentModule
	LLM(opts ...dagger.LLMOpts) *dagger.LLM
}
