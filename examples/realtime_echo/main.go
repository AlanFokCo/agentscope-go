package main

import (
	"context"
	"fmt"

	as "github.com/alanfokco/agentscope-go/pkg/agentscope"
	asagent "github.com/alanfokco/agentscope-go/pkg/agentscope/agent"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/realtime"
)

// This example demonstrates the minimal use of the Realtime interface with EchoClient as an echo server.

type RealtimeAgent struct {
	asagent.AgentBase
	client realtime.Client
}

func NewRealtimeAgent(c realtime.Client) *RealtimeAgent {
	return &RealtimeAgent{
		AgentBase: asagent.NewAgentBase(),
		client:    c,
	}
}

func (a *RealtimeAgent) Reply(ctx context.Context, args ...any) (*message.Msg, error) {
	var initMsg *message.Msg
	if len(args) > 0 {
		if m, ok := args[0].(*message.Msg); ok {
			initMsg = m
		}
	}
	if initMsg == nil {
		initMsg = message.NewMsg("user", message.RoleUser, "Hello realtime echo")
	}

	stream, err := a.client.Start(ctx, []*message.Msg{initMsg})
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	msg, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	if msg != nil {
		_ = a.Print(ctx, msg)
	}
	return msg, nil
}

func (a *RealtimeAgent) Observe(ctx context.Context, msgs []*message.Msg) error {
	_ = ctx
	_ = msgs
	return nil
}

func main() {
	as.Init()

	echoClient := realtime.EchoClient{}
	rt := NewRealtimeAgent(echoClient)

	ctx := context.Background()
	msg := message.NewMsg("user", message.RoleUser, "This message will be echoed back.")
	reply, err := rt.Reply(ctx, msg)
	if err != nil {
		fmt.Println("RealtimeAgent error:", err)
		return
	}
	if txt := reply.GetTextContent("\n"); txt != nil {
		fmt.Println("realtime echo:", *txt)
	}
}

