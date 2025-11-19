package dagger

import (
	"context"
	
	"dagger/workflow-scanner/internal/dagger"
)

type Directory interface {
	WithoutDirectory(path string) *dagger.Directory
}

type Env interface {
	WithStringInput(name, value, description string) Env
	WithWorkspaceInput(name string, workspace Workspace, description string) Env
	WithWorkspaceOutput(name, description string) Env
	WithStringOutput(name, description string) Env
	Output(name string) EnvOutput
}

type EnvOutput interface {
	AsString(ctx context.Context) (string, error)
	AsWorkspace() Workspace
}

type Workspace interface {
	Source() *dagger.Directory
}

type CurrentModule interface {
	Source() ModuleSource
}

type ModuleSource interface {
	File(path string) *dagger.File
}

type LLM interface {
	WithEnv(env Env) LLMWithEnv
	WithPromptFile(file *dagger.File) LLMWithEnv
}

type LLMWithEnv interface {
	WithEnv(env Env) LLMWithEnv
	WithPromptFile(file *dagger.File) LLMWithEnv
	Env() Env
}