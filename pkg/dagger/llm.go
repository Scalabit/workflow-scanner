package dagger

//go:generate mockgen -source=llm.go -destination=../../mocks/llm_mock.go -package=mocks LLM

import (
	"context"
	"workflow-scanner/internal/dagger"
)

type LLM interface {
	LastReply(ctx context.Context) (string, error)
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

func (llmImpl *LLMImpl) LastReply(ctx context.Context) (string, error) {
	return llmImpl.internal.LastReply(ctx)
}
