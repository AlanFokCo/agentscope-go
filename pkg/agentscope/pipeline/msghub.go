package pipeline

import (
	"context"
	"fmt"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/agent"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

// MsgHub manages message routing between multiple agents, conceptually similar to Python's MsgHub.
type MsgHub struct {
	agents map[string]agent.Agent
}

func NewMsgHub() *MsgHub {
	return &MsgHub{
		agents: make(map[string]agent.Agent),
	}
}

func (h *MsgHub) Register(name string, a agent.Agent) {
	if a == nil || name == "" {
		return
	}
	h.agents[name] = a
}

func (h *MsgHub) Get(name string) agent.Agent {
	return h.agents[name]
}

// Broadcast sends the message to all registered agents via their Observe method.
func (h *MsgHub) Broadcast(ctx context.Context, msg *message.Msg) error {
	for _, a := range h.agents {
		if err := a.Observe(ctx, []*message.Msg{msg}); err != nil {
			return err
		}
	}
	return nil
}

// RequestReply sends a message from one logical agent to another agent's Reply method and returns the reply.
func (h *MsgHub) RequestReply(
	ctx context.Context,
	from string,
	to string,
	msg *message.Msg,
) (*message.Msg, error) {
	src := h.agents[from]
	dst := h.agents[to]
	if dst == nil {
		return nil, fmt.Errorf("msghub: agent %q not found", to)
	}
	_ = src // Source is not used for now; kept for future extension.
	return dst.Reply(ctx, msg)
}

