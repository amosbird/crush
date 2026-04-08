package agent

import (
	"context"
	_ "embed"

	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/config"
)

//go:embed templates/coder.md.tpl
var coderPromptTmpl []byte

//go:embed templates/planner.md.tpl
var plannerPromptTmpl []byte

//go:embed templates/superpowers.md.tpl
var superpowersPromptTmpl []byte

//go:embed templates/gsd.md.tpl
var gsdPromptTmpl []byte

//go:embed templates/task.md.tpl
var taskPromptTmpl []byte

//go:embed templates/worker.md.tpl
var workerPromptTmpl []byte

//go:embed templates/initialize.md.tpl
var initializePromptTmpl []byte

func coderPrompt(opts ...prompt.Option) (*prompt.Prompt, error) {
	systemPrompt, err := prompt.NewPrompt("coder", string(coderPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

func plannerPrompt(opts ...prompt.Option) (*prompt.Prompt, error) {
	systemPrompt, err := prompt.NewPrompt("planner", string(plannerPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

func superpowersPrompt(opts ...prompt.Option) (*prompt.Prompt, error) {
	systemPrompt, err := prompt.NewPrompt("superpowers", string(superpowersPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

func gsdPrompt(opts ...prompt.Option) (*prompt.Prompt, error) {
	systemPrompt, err := prompt.NewPrompt("gsd", string(gsdPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

func taskPrompt(opts ...prompt.Option) (*prompt.Prompt, error) {
	systemPrompt, err := prompt.NewPrompt("task", string(taskPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

func workerPrompt(opts ...prompt.Option) (*prompt.Prompt, error) {
	systemPrompt, err := prompt.NewPrompt("worker", string(workerPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

// agentPromptFunc maps agent IDs to their prompt factory functions.
var agentPromptFunc = map[string]func(...prompt.Option) (*prompt.Prompt, error){
	config.AgentCoder:       coderPrompt,
	config.AgentPlanner:     plannerPrompt,
	config.AgentSuperpowers: superpowersPrompt,
	config.AgentGSD:         gsdPrompt,
}

func InitializePrompt(cfg *config.ConfigStore) (string, error) {
	systemPrompt, err := prompt.NewPrompt("initialize", string(initializePromptTmpl))
	if err != nil {
		return "", err
	}
	return systemPrompt.Build(context.Background(), "", "", cfg)
}
