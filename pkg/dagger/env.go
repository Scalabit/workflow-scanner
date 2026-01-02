package dagger

//go:generate mockgen -source=env.go -destination=../../mocks/env_mock.go -package=mocks Env

import (
	"fmt"
	"workflow-scanner/internal/dagger"
)

type Env interface {
	WithStringInput(name, value, description string) Env
	WithDirectoryInput(name string, value *dagger.Directory, description string) Env
	WithDirectoryOutput(name, description string) Env
	WithStringOutput(name, description string) Env
	Output(name string) Binding
	GetEnv() *dagger.Env
}

type EnvImpl struct {
	internal *dagger.Env
}

func NewEnvImpl(env *dagger.Env) Env {
	return &EnvImpl{
		internal: env,
	}
}

func (envImpl *EnvImpl) WithStringInput(name, value, description string) Env {
	return &EnvImpl{internal: envImpl.internal.WithStringInput(name, value, description)}
}

func (envImpl *EnvImpl) WithDirectoryInput(name string, value *dagger.Directory, description string) Env {
	return &EnvImpl{internal: envImpl.internal.WithDirectoryInput(name, value, description)}
}

func (envImpl *EnvImpl) WithDirectoryOutput(name, description string) Env {
	return &EnvImpl{internal: envImpl.internal.WithDirectoryOutput(name, description)}
}

func (envImpl *EnvImpl) WithStringOutput(name, description string) Env {
	return &EnvImpl{internal: envImpl.internal.WithStringOutput(name, description)}
}

func (envImpl *EnvImpl) Output(name string) Binding {
	fmt.Println("TEST NAME: ", name)
	return &BindingImpl{internal: envImpl.internal.Output(name)}
}

func (envImpl *EnvImpl) GetEnv() *dagger.Env {
	return envImpl.internal
}
