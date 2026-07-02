package runtime

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/agent"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

// ErrPoolClosed is returned by Submit when the pool has been closed.
var ErrPoolClosed = errors.New("agent pool is closed")

// AgentFactory creates a fresh agent instance for a pool worker.
type AgentFactory func() agent.Agent

// PoolResult holds the outcome of processing one submitted input.
type PoolResult struct {
	Input    string
	Output   *message.Msg
	Err      error
	Duration time.Duration
}

// PoolOption configures an AgentPool.
type PoolOption func(*AgentPool)

// Workers sets the number of worker goroutines. Defaults to 1.
func Workers(n int) PoolOption {
	return func(p *AgentPool) {
		if n > 0 {
			p.workers = n
		}
	}
}

// QueueSize sets the buffered work channel capacity. Defaults to the number of
// workers.
func QueueSize(n int) PoolOption {
	return func(p *AgentPool) {
		if n > 0 {
			p.queueSize = n
		}
	}
}

type job struct {
	ctx    context.Context
	input  string
	result chan PoolResult
}

// AgentPool is a work-queue that dispatches inputs to a fixed set of worker
// goroutines, each owning an agent created from the factory.
type AgentPool struct {
	factory   AgentFactory
	workers   int
	queueSize int

	jobs      chan job
	closedCh  chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// NewAgentPool constructs an AgentPool and starts its workers.
func NewAgentPool(factory AgentFactory, opts ...PoolOption) *AgentPool {
	p := &AgentPool{
		factory:  factory,
		workers:  1,
		closedCh: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(p)
	}
	if p.queueSize <= 0 {
		p.queueSize = p.workers
	}
	p.jobs = make(chan job, p.queueSize)
	p.start()
	return p
}

func (p *AgentPool) start() {
	p.startOnce.Do(func() {
		for i := 0; i < p.workers; i++ {
			p.wg.Add(1)
			go p.worker()
		}
	})
}

func (p *AgentPool) worker() {
	defer p.wg.Done()
	a := p.factory()
	for j := range p.jobs {
		start := time.Now()
		out, err := a.Reply(j.ctx, j.input)
		j.result <- PoolResult{
			Input:    j.input,
			Output:   out,
			Err:      err,
			Duration: time.Since(start),
		}
		close(j.result)
	}
}

// Submit enqueues an input for processing and returns a channel that will
// receive its single result. Returns ErrPoolClosed if the pool has been closed.
func (p *AgentPool) Submit(ctx context.Context, input string) (<-chan PoolResult, error) {
	result := make(chan PoolResult, 1)
	j := job{ctx: ctx, input: input, result: result}
	select {
	case p.jobs <- j:
		return result, nil
	case <-p.closedCh:
		return nil, ErrPoolClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close stops accepting new work and waits for in-flight jobs to finish.
func (p *AgentPool) Close() {
	p.closeOnce.Do(func() {
		close(p.closedCh)
		close(p.jobs)
	})
	p.wg.Wait()
}
