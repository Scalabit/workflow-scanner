package dagger

//go:generate mockgen -source=llm.go -destination=../../mocks/llm_mock.go -package=mocks LLM

import "dagger/workflow-scanner/internal/dagger"

type LLM interface {
	WithEnv(env Env) LLM
	WithPromptFile(file *dagger.File) LLM
	Env() Env
}

type LLMImpl struct {
	internal *dagger.LLM
}

func (llmImpl *LLMImpl) WithEnv(env Env) LLM {
	return &LLMImpl{
		internal: llmImpl.internal.WithEnv(env.GetEnv()),
	}
}

func (llmImpl *LLMImpl) WithPromptFile(file *dagger.File) LLM {
	return &LLMImpl{internal: llmImpl.internal.WithPromptFile(file)}
}

func (llmImpl *LLMImpl) Env() Env {
	return &EnvImpl{internal: llmImpl.internal.Env()}
}
