package dagger

import (
	"context"
	
	internalDagger "dagger/workflow-scanner/internal/dagger"
)

// ClientAdapter wraps the internal Dagger client to implement the Client interface
type ClientAdapter struct {
	client *internalDagger.Client
}

func NewClientAdapter(client *internalDagger.Client) Client {
	return &ClientAdapter{client: client}
}

func (c *ClientAdapter) Container(opts ...internalDagger.ContainerOpts) Container {
	return &ContainerAdapter{container: c.client.Container(opts...)}
}

func (c *ClientAdapter) Env(opts ...internalDagger.EnvOpts) Env {
	return &EnvAdapter{env: c.client.Env(opts...)}
}

func (c *ClientAdapter) Workspace(source Directory) Workspace {
	sourceAdapter := source.(*DirectoryAdapter)
	return &WorkspaceAdapter{workspace: c.client.Workspace(sourceAdapter.directory)}
}

func (c *ClientAdapter) CurrentModule() CurrentModule {
	return &CurrentModuleAdapter{module: c.client.CurrentModule()}
}

func (c *ClientAdapter) LLM(opts ...internalDagger.LLMOpts) LLM {
	return &LLMAdapter{llm: c.client.LLM(opts...)}
}

// ContainerAdapter wraps the internal Container
type ContainerAdapter struct {
	container *internalDagger.Container
}

func (c *ContainerAdapter) Directory(path string, opts ...internalDagger.ContainerDirectoryOpts) Directory {
	return &DirectoryAdapter{directory: c.container.Directory(path, opts...)}
}

func (c *ContainerAdapter) From(address string) Container {
	return &ContainerAdapter{container: c.container.From(address)}
}

func (c *ContainerAdapter) Stdout(ctx context.Context) (string, error) {
	return c.container.Stdout(ctx)
}

func (c *ContainerAdapter) WithExec(args []string, opts ...internalDagger.ContainerWithExecOpts) Container {
	return &ContainerAdapter{container: c.container.WithExec(args, opts...)}
}

func (c *ContainerAdapter) WithDirectory(path string, source Directory, opts ...internalDagger.ContainerWithDirectoryOpts) Container {
	sourceAdapter := source.(*DirectoryAdapter)
	return &ContainerAdapter{container: c.container.WithDirectory(path, sourceAdapter.directory, opts...)}
}

func (c *ContainerAdapter) WithWorkdir(path string, opts ...internalDagger.ContainerWithWorkdirOpts) Container {
	return &ContainerAdapter{container: c.container.WithWorkdir(path, opts...)}
}

// DirectoryAdapter wraps the internal Directory
type DirectoryAdapter struct {
	directory *internalDagger.Directory
}

func NewDirectoryAdapter(directory *internalDagger.Directory) Directory {
	return &DirectoryAdapter{directory: directory}
}

func (d *DirectoryAdapter) WithoutDirectory(path string) Directory {
	return &DirectoryAdapter{directory: d.directory.WithoutDirectory(path)}
}

// GetInternal extracts the internal directory for use with GitHub API
func (d *DirectoryAdapter) GetInternal() *internalDagger.Directory {
	return d.directory
}

// EnvAdapter and other adapters...
type EnvAdapter struct {
	env *internalDagger.Env
}

func (e *EnvAdapter) WithStringInput(name, value, description string) Env {
	return &EnvAdapter{env: e.env.WithStringInput(name, value, description)}
}

func (e *EnvAdapter) WithWorkspaceInput(name string, workspace Workspace, description string) Env {
	workspaceAdapter := workspace.(*WorkspaceAdapter)
	return &EnvAdapter{env: e.env.WithWorkspaceInput(name, workspaceAdapter.workspace, description)}
}

func (e *EnvAdapter) WithWorkspaceOutput(name, description string) Env {
	return &EnvAdapter{env: e.env.WithWorkspaceOutput(name, description)}
}

func (e *EnvAdapter) WithStringOutput(name, description string) Env {
	return &EnvAdapter{env: e.env.WithStringOutput(name, description)}
}

func (e *EnvAdapter) Output(name string) EnvOutput {
	return &EnvOutputAdapter{output: e.env.Output(name)}
}

type EnvOutputAdapter struct {
	output *internalDagger.Binding
}

func (e *EnvOutputAdapter) AsString(ctx context.Context) (string, error) {
	return e.output.AsString(ctx)
}

func (e *EnvOutputAdapter) AsWorkspace() Workspace {
	return &WorkspaceAdapter{workspace: e.output.AsWorkspace()}
}

type WorkspaceAdapter struct {
	workspace *internalDagger.Workspace
}

func (w *WorkspaceAdapter) Source() Directory {
	return &DirectoryAdapter{directory: w.workspace.Source()}
}

type CurrentModuleAdapter struct {
	module *internalDagger.CurrentModule
}

func (c *CurrentModuleAdapter) Source() ModuleSource {
	return &ModuleSourceAdapter{source: c.module.Source()}
}

type ModuleSourceAdapter struct {
	source *internalDagger.Directory
}

func (m *ModuleSourceAdapter) File(path string) *internalDagger.File {
	return m.source.File(path)
}

type LLMAdapter struct {
	llm *internalDagger.LLM
}

func (l *LLMAdapter) WithEnv(env Env) LLMWithEnv {
	envAdapter := env.(*EnvAdapter)
	return &LLMWithEnvAdapter{llmWithEnv: l.llm.WithEnv(envAdapter.env)}
}

func (l *LLMAdapter) WithPromptFile(file *internalDagger.File) LLMWithEnv {
	return &LLMWithEnvAdapter{llmWithEnv: l.llm.WithPromptFile(file)}
}

type LLMWithEnvAdapter struct {
	llmWithEnv *internalDagger.LLM
}

func (l *LLMWithEnvAdapter) WithEnv(env Env) LLMWithEnv {
	envAdapter := env.(*EnvAdapter)
	return &LLMWithEnvAdapter{llmWithEnv: l.llmWithEnv.WithEnv(envAdapter.env)}
}

func (l *LLMWithEnvAdapter) WithPromptFile(file *internalDagger.File) LLMWithEnv {
	return &LLMWithEnvAdapter{llmWithEnv: l.llmWithEnv.WithPromptFile(file)}
}

func (l *LLMWithEnvAdapter) Env() Env {
	return &EnvAdapter{env: l.llmWithEnv.Env()}
}