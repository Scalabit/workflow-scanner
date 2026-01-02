package dagger

//go:generate mockgen -source=env.go -destination=../../mocks/env_mock.go -package=mocks Env

import (
	"workflow-scanner/internal/dagger"
)

type Env interface {
	WithDirectoryInput(name string, value *dagger.Directory, description string) Env
	WithDirectoryOutput(name string, description string) Env
	WithStringInput(name, value, description string) Env
	WithWorkspaceInput(name string, value Workspace, description string) Env
	WithWorkspaceOutput(name, description string) Env
	WithStringOutput(name, description string) Env
	Output(name string) Binding
	GetEnv() *dagger.Env
	WithSecretInput(name string, secret *dagger.Secret, description string) Env
}

type EnvImpl struct {
	internal *dagger.Env
}

func NewEnvImpl(env *dagger.Env) Env {
	return &EnvImpl{
		internal: env,
	}
}

func (envImpl *EnvImpl) WithSecretInput(name string, secret *dagger.Secret, description string) Env {
	return &EnvImpl{internal: envImpl.internal.WithSecretInput(name, secret, description)}
}

func (envImpl *EnvImpl) WithStringInput(name, value, description string) Env {
	return &EnvImpl{internal: envImpl.internal.WithStringInput(name, value, description)}
}

func (envImpl *EnvImpl) WithWorkspaceInput(name string, value Workspace, description string) Env {
	return &EnvImpl{
		internal: envImpl.internal.WithWorkspaceInput(
			name,
			value.GetWorkspace(),
			description,
		),
	}
}

func (envImpl *EnvImpl) WithWorkspaceOutput(name, description string) Env {
	return &EnvImpl{internal: envImpl.internal.WithWorkspaceOutput(name, description)}
}

func (envImpl *EnvImpl) WithStringOutput(name, description string) Env {
	return &EnvImpl{internal: envImpl.internal.WithStringOutput(name, description)}
}

func (envImpl *EnvImpl) Output(name string) Binding {
	return &BindingImpl{internal: envImpl.internal.Output(name)}
}

func (envImpl *EnvImpl) WithDirectoryInput(name string, value *dagger.Directory, description string) Env {
	return &EnvImpl{internal: envImpl.internal.WithDirectoryInput(name, value, description)}
}

func (envImpl *EnvImpl) WithDirectoryOutput(name string, description string) Env {
	return &EnvImpl{internal: envImpl.internal.WithDirectoryOutput(name, description)}
}

func (envImpl *EnvImpl) GetEnv() *dagger.Env {
	return envImpl.internal
}
